package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
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
	"github.com/styygeli/echonetgo/internal/model"
	mqttpub "github.com/styygeli/echonetgo/internal/mqtt"
	"github.com/styygeli/echonetgo/internal/poller"
	"github.com/styygeli/echonetgo/internal/specs"
)

// version is set at build time via:
//
//	-ldflags "-X main.version=<release-version>"
//
// Defaults to "dev" for local builds.
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

	go cache.Start(ctx, cfg, deviceSpecs, transport, func() { readiness.MarkReady("poller") })

	if mqttPub != nil {
		setupCommander(ctx, cfg, cache, transport, mqttPub, readiness)
	}

	server, errCh := setupHTTPServer(cfg, cache, deviceSpecs, readiness, log)

	handleShutdown(cancel, mqttPub, server, errCh, log)
}

// setupMQTT initializes the MQTT publisher and configures the cache to automatically
// publish updates when device state changes are detected.
func setupMQTT(cfg *config.Config, cache *poller.Cache, readiness *api.Readiness, log *logging.Logger) (*mqttpub.Publisher, error) {
	if !cfg.MQTTEnabled() {
		return nil, nil
	}
	mqttPub, err := mqttpub.NewPublisher(cfg.MQTT, version)
	if err != nil {
		return nil, err
	}
	log.Infof("MQTT publishing to %s", cfg.MQTT.Broker)
	cache.SetOnUpdate(func(dev config.Device, info echonet.DeviceInfo, metrics map[string]echonet.MetricValue, metricSpecs []specs.MetricSpec, writable map[byte]struct{}, climateSpec *specs.ClimateSpec, lightSpec *specs.LightSpec, success bool) {
		mqttPub.PublishDeviceState(dev, info, metrics, metricSpecs, writable, climateSpec, lightSpec, success)
	})
	readiness.Register("commander")
	return mqttPub, nil
}

func setupEchonetTransport(cfg *config.Config, cache *poller.Cache, log *logging.Logger) *echonet.Transport {
	transport := echonet.NewTransport(cfg.StrictSourcePort3610)

	if len(cfg.Devices) > 0 {
		ipToName := make(map[string]string, len(cfg.Devices))
		for _, d := range cfg.Devices {
			ipToName[d.IP] = d.Name
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
		func(ip string, seoj [3]byte, props []model.GetResProperty) {
			dev, ok := cache.FindDeviceByIPAndEOJ(ip, seoj, cfg.Devices)
			if !ok {
				return
			}
			devSpecs, ok := cache.GetDeviceSpecs(dev)
			if !ok {
				return
			}
			epcs := make([]byte, 0, len(props))
			for _, p := range props {
				epcs = append(epcs, p.EPC)
			}
			cache.RecordPush(dev, epcs)
			infEPCs := make(map[byte]struct{}, len(props))
			for _, p := range props {
				infEPCs[p.EPC] = struct{}{}
			}
			var relevantSpecs []specs.MetricSpec
			for _, s := range devSpecs {
				if _, ok := infEPCs[s.EPC]; ok {
					relevantSpecs = append(relevantSpecs, s)
				}
			}
			metrics := echonet.ParsePropsToMetrics(props, relevantSpecs)
			if len(metrics) == 0 {
				return
			}
			cache.UpdateFromINF(dev, metrics)
		})
	for _, dev := range cfg.Devices {
		notifHandler.RegisterDevice(dev.IP)
	}
	go notifHandler.Run(ctx)
}

func setupCommander(ctx context.Context, cfg *config.Config, cache *poller.Cache, transport *echonet.Transport, mqttPub *mqttpub.Publisher, readiness *api.Readiness) {
	echonetClient := echonet.NewClient(transport, cfg.ScrapeTimeoutSec)
	commander := mqttpub.NewCommander(echonetClient, cache, cfg, cfg.MQTT.TopicPrefix)
	go commander.Run(ctx, mqttPub.Client(), func() { readiness.MarkReady("commander") })
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

// handleShutdown blocks until an OS termination signal is received or a fatal HTTP server
// error occurs, then coordinates a graceful shutdown of all background processes.
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
