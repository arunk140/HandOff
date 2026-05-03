package watcher

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/fsnotify/fsnotify"

	"handoff/config"
)

type Watcher struct {
	configPath string
	cfgPtr     *atomic.Pointer[config.Config]
	loader     func(string) (*config.Config, error)
}

func New(configPath string, cfgPtr *atomic.Pointer[config.Config], loader func(string) (*config.Config, error)) *Watcher {
	return &Watcher{
		configPath: configPath,
		cfgPtr:     cfgPtr,
		loader:     loader,
	}
}

func (w *Watcher) Start(ctx context.Context) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	if err := fsw.Add(w.configPath); err != nil {
		fsw.Close()
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGUSR1)

	reload := func() {
		slog.Info("reloading config", "path", w.configPath)
		cfg, err := w.loader(w.configPath)
		if err != nil {
			slog.Error("config reload failed", "error", err)
			return
		}
		w.cfgPtr.Store(cfg)
		slog.Info("config reloaded successfully")
	}

	go func() {
		defer fsw.Close()
		defer signal.Stop(sigCh)

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-fsw.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					reload()
				}
			case err, ok := <-fsw.Errors:
				if !ok {
					return
				}
				slog.Error("fsnotify error", "error", err)
			case <-sigCh:
				reload()
			}
		}
	}()

	return nil
}
