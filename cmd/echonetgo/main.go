// echonetgo is a Go service for ECHONET Lite devices (polling, cache, API).
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/styygeli/echonetgo/internal/api"
	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/logging"
	"github.com/styygeli/echonetgo/internal/poller"
	"github.com/styygeli/echonetgo/internal/specs"
)

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Start polling in background so the API listener comes up immediately.
	go cache.Start(ctx, cfg, deviceSpecs)

	srv := &api.Server{
		ListenAddr: cfg.ListenAddr,
		GetState:   func() interface{} { return cache.StateForAPI(cfg) },
	}

	server := &http.Server{Addr: cfg.ListenAddr, Handler: srv.Handler()}
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
		if err := server.Shutdown(context.Background()); err != nil {
			log.Warnf("HTTP shutdown: %v", err)
		}
	case err := <-errCh:
		log.Fatalf("HTTP server: %v", err)
	}
}
