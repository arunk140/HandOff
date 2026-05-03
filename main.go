package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"handoff/config"
	"handoff/logging"
	"handoff/proxy"
	"handoff/watcher"
)

func main() {
	configPath := flag.String("c", "config.yaml", "path to config file")
	secretsPath := flag.String("secrets", "", "path to secrets file")
	flag.Parse()

	logger := logging.Default()
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	secrets, err := config.LoadSecrets(*secretsPath)
	if err != nil {
		slog.Warn("failed to load secrets", "error", err)
		secrets = map[string]string{}
	}

	cfgPtr := &atomic.Pointer[config.Config]{}
	cfgPtr.Store(cfg)

	handler := proxy.NewProxyHandler(cfgPtr, secrets)

	w := watcher.New(*configPath, cfgPtr, func(path string) (*config.Config, error) {
		return config.Load(path)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		slog.Error("failed to start watcher", "error", err)
		os.Exit(1)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Listen.Host, cfg.Listen.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("shutting down", "signal", sig.String())
		cancel()
		_ = server.Shutdown(context.Background())
	}()

	slog.Info("handoff proxy starting", "addr", addr, "tls", cfg.Listen.TLS.Enabled)

	if cfg.Listen.TLS.Enabled {
		err = server.ListenAndServeTLS(cfg.Listen.TLS.CertFile, cfg.Listen.TLS.KeyFile)
	} else {
		err = server.ListenAndServe()
	}

	if err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
