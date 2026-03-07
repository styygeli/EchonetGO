// echonetgo is a Go service for ECHONET Lite devices (polling, cache, API).
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/styygeli/echonetgo/internal/api"
	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/logging"
	mqttpub "github.com/styygeli/echonetgo/internal/mqtt"
	"github.com/styygeli/echonetgo/internal/poller"
	"github.com/styygeli/echonetgo/internal/specs"
)

const addonVersion = "0.1.34"

func main() {
	log := logging.New("main")
	logging.SetLevelFromEnv()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	deviceSpecs, err := specs.Load(cfg.SpecsDir)
	if err != nil {
		log.Fatalf("specs: %v", err)
	}

	cache := poller.NewCache()

	var mqttPub *mqttpub.Publisher
	if cfg.MQTTEnabled() {
		mqttPub, err = mqttpub.NewPublisher(cfg.MQTT, addonVersion)
		if err != nil {
			log.Warnf("MQTT disabled: %v", err)
		} else {
			log.Infof("MQTT publishing to %s", cfg.MQTT.Broker)
			cache.SetOnUpdate(func(dev config.Device, info echonet.DeviceInfo, metrics map[string]echonet.MetricValue, metricSpecs []specs.MetricSpec, success bool) {
				mqttPub.PublishDeviceState(dev, info, metrics, metricSpecs, success)
			})
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go cache.Start(ctx, cfg, deviceSpecs)

	srv := &api.Server{
		ListenAddr: cfg.ListenAddr,
		GetState:   func() any { return cache.StateForAPI(cfg) },
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
