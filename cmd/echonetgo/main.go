// echonetgo is a Go service for ECHONET Lite devices (polling, cache, API).
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

	var mqttPub *mqttpub.Publisher
	if cfg.MQTTEnabled() {
		mqttPub, err = mqttpub.NewPublisher(cfg.MQTT, version)
		if err != nil {
			log.Warnf("MQTT disabled: %v", err)
		} else {
			log.Infof("MQTT publishing to %s", cfg.MQTT.Broker)
			cache.SetOnUpdate(func(dev config.Device, info echonet.DeviceInfo, metrics map[string]echonet.MetricValue, metricSpecs []specs.MetricSpec, writable map[byte]struct{}, climateSpec *specs.ClimateSpec, lightSpec *specs.LightSpec, success bool) {
				mqttPub.PublishDeviceState(dev, info, metrics, metricSpecs, writable, climateSpec, lightSpec, success)
			})
			readiness.Register("commander")
			// Commander will be started after ctx is created (below)
		}
	}

	transport := echonet.NewTransport(cfg.StrictSourcePort3610)
	defer transport.Close()

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if cfg.NotificationsEnabled {
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
				// Only pass specs for EPCs present in the INF frame to
				// avoid "missing EPC" warnings for every other metric.
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

	go cache.Start(ctx, cfg, deviceSpecs, transport, func() { readiness.MarkReady("poller") })
	if mqttPub != nil {
		echonetClient := echonet.NewClient(transport, cfg.ScrapeTimeoutSec)
		commander := mqttpub.NewCommander(echonetClient, cache, cfg, cfg.MQTT.TopicPrefix)
		go commander.Run(ctx, mqttPub.Client(), func() { readiness.MarkReady("commander") })
	}

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

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	select {
	case <-sigCh:
		log.Infof("Shutting down...")
		cancel()
		if mqttPub != nil {
			mqttPub.Disconnect()
		}
		if err := server.Shutdown(context.Background()); err != nil {
			log.Warnf("HTTP shutdown: %v", err)
		}
	case err := <-errCh:
		log.Fatalf("HTTP server: %v", err)
	}
}
