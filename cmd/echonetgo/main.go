package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/styygeli/echonetgo/internal/api"
	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/logging"
	echonetmetrics "github.com/styygeli/echonetgo/internal/metrics"
	mqttpub "github.com/styygeli/echonetgo/internal/mqtt"
	"github.com/styygeli/echonetgo/internal/poller"
	"github.com/styygeli/echonetgo/internal/specs"
)

// version is injected at build time via -ldflags "-X main.version=...". Defaults to "dev".
var version = "dev"

func main() {
	log := logging.New("main")
	logging.SetLevelFromEnv()
	log.Infof("EchonetGO %s starting", version)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	deviceSpecs, err := specs.Load(cfg.SpecsDir)
	if err != nil {
		log.Fatalf("specs: %v", err)
	}

	cache := poller.NewCache()
	readiness := api.NewReadiness()
	readiness.Register("poller")

	mqttPub, err := setupMQTT(cfg, cache, readiness, log)
	if err != nil && cfg.MQTTEnabled() {
		log.Warnf("MQTT disabled: %v", err)
	}

	transport := setupEchonetTransport(cfg, cache, log)
	defer transport.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	setupNotifications(ctx, cfg, cache, transport, log)

	scheduler := poller.NewScheduler(cache)
	go scheduler.Start(ctx, cfg, deviceSpecs, transport, func() { readiness.MarkReady("poller") })

	if mqttPub != nil {
		setupCommander(ctx, cfg, cache, transport, mqttPub, readiness, scheduler.Reconciler())
	}

	server, errCh := setupHTTPServer(cfg, cache, deviceSpecs, readiness, log)

	handleShutdown(cancel, mqttPub, server, errCh, log)
}

// setupMQTT creates the publisher and wires the cache to publish on state change.
func setupMQTT(cfg *config.Config, cache *poller.Cache, readiness *api.Readiness, log *logging.Logger) (*mqttpub.Publisher, error) {
	if !cfg.MQTTEnabled() {
		return nil, nil
	}
	mqttPub, err := mqttpub.NewPublisher(cfg.MQTT, version)
	if err != nil {
		return nil, err
	}
	log.Infof("MQTT publishing to %s", cfg.MQTT.Broker)
	cache.SetOnUpdate(mqttPub.EnqueueDeviceState)
	readiness.Register("commander")
	return mqttPub, nil
}

func setupEchonetTransport(cfg *config.Config, cache *poller.Cache, log *logging.Logger) *echonet.Transport {
	transport := echonet.NewTransport(cfg.StrictSourcePort3610)

	if len(cfg.Devices) > 0 {
		namesByIP := make(map[string][]string)
		seen := make(map[string]map[string]bool)
		for _, d := range cfg.Devices {
			if seen[d.IP] == nil {
				seen[d.IP] = make(map[string]bool)
			}
			if !seen[d.IP][d.Name] {
				seen[d.IP][d.Name] = true
				namesByIP[d.IP] = append(namesByIP[d.IP], d.Name)
			}
		}
		ipToName := make(map[string]string, len(namesByIP))
		for ip, names := range namesByIP {
			ipToName[ip] = strings.Join(names, "/")
		}
		transport.SetNameResolver(func(ip string) string {
			return ipToName[ip]
		})
	}

	if cfg.NotificationsEnabled {
		infChan := make(chan echonet.UDPFrame, 256)
		transport.SetNotificationChan(infChan)
		joined := transport.JoinMulticast(cfg.MulticastInterfaces)
		if len(joined) > 0 {
			log.Infof("multicast: listening on %d interface(s)", len(joined))
		} else {
			log.Warnf("multicast: no interfaces joined; INF notifications may not be received")
		}
	}
	if cfg.ForcePolling {
		cache.SetForcePolling(true)
		log.Infof("force_polling enabled: STATMAP will be ignored, all EPCs polled normally")
	}
	return transport
}

func setupNotifications(ctx context.Context, cfg *config.Config, cache *poller.Cache, transport *echonet.Transport, log *logging.Logger) {
	if !cfg.NotificationsEnabled {
		return
	}
	notifHandler := echonet.NewNotificationHandler(transport.NotificationChan(), transport,
		func(ip string, seoj [3]byte, props []echonet.Property) {
			cache.IngestNotification(ip, seoj, props, cfg.Devices)
		})
	for _, dev := range cfg.Devices {
		notifHandler.RegisterDevice(dev.IP)
	}
	go notifHandler.Run(ctx)
}

func setupCommander(ctx context.Context, cfg *config.Config, cache *poller.Cache, transport *echonet.Transport, mqttPub *mqttpub.Publisher, readiness *api.Readiness, reconciler mqttpub.CapabilityReconciler) {
	echonetClient := echonet.NewClient(transport, cfg.ScrapeTimeoutSec)
	if rec, ok := reconciler.(*poller.CapabilityReconciler); ok && rec != nil {
		rec.SetClient(echonetClient)
	}
	commander := mqttpub.NewCommander(echonetClient, cache, cfg, cfg.MQTT.TopicPrefix)
	if reconciler != nil {
		commander.SetReconciler(reconciler)
	}
	go commander.Run(ctx, mqttPub, func() { readiness.MarkReady("commander") })
}

func setupHTTPServer(cfg *config.Config, cache *poller.Cache, deviceSpecs map[string]*specs.DeviceSpec, readiness *api.Readiness, log *logging.Logger) (*http.Server, chan error) {
	srv := &api.Server{
		ListenAddr: cfg.ListenAddr,
		Readiness:  readiness,
	}
	if cfg.MetricsEnabled {
		registry := prometheus.NewRegistry()
		registry.MustRegister(
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
			echonetmetrics.NewCollector(cfg, cache, deviceSpecs),
		)
		srv.MetricsHandler = promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
		log.Infof("/metrics endpoint enabled")
	}

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      srv.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	log.Infof("Listening on %s", cfg.ListenAddr)
	return server, errCh
}

// handleShutdown blocks until a termination signal or fatal server error, then shuts down cleanly.
func handleShutdown(cancel context.CancelFunc, mqttPub *mqttpub.Publisher, server *http.Server, errCh chan error, log *logging.Logger) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	select {
	case <-sigCh:
		log.Infof("Shutting down...")
	case err := <-errCh:
		log.Errorf("HTTP server error: %v", err)
	}

	cancel()
	if mqttPub != nil {
		mqttPub.Disconnect()
	}
	ctx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Warnf("HTTP shutdown: %v", err)
	}
}
