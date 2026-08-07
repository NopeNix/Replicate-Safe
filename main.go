package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/replicate/replicate-go"

	"github.com/phil/Replicate-Safe/internal/config"
	"github.com/phil/Replicate-Safe/internal/download"
	"github.com/phil/Replicate-Safe/internal/state"
	syncpkg "github.com/phil/Replicate-Safe/internal/sync"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := newLogger(cfg.LogLevel)
	log.Info("starting replicate-safe",
		"output_dir", cfg.OutputDir,
		"state_file", cfg.StateFile,
		"poll_interval", cfg.PollInterval,
		"http_timeout", cfg.HTTPTimeout,
		"write_metadata", 	cfg.WriteMetadata,
	)

	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return err
	}

	st, err := state.Load(cfg.StateFile)
	if err != nil {
		return err
	}
	log.Info("state loaded", "seen", len(st.SeenIDs), "last", st.LastCreatedAt)

	client, err := replicate.NewClient(
		replicate.WithToken(cfg.APIToken),
		replicate.WithUserAgent("replicate-safe/1.0"),
		replicate.WithHTTPClient(&http.Client{Timeout: cfg.HTTPTimeout}),
	)
	if err != nil {
		return err
	}

	dl := download.New(cfg.HTTPTimeout, cfg.APIToken, log)
	runner := syncpkg.New(client, st, dl, cfg.OutputDir, 	cfg.WriteMetadata, log)

	rootCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	if err := runPass(rootCtx, log, runner, st, cfg.StateFile); err != nil {
		log.Error("sync pass failed", "err", err)
	}

	for {
		select {
		case <-rootCtx.Done():
			log.Info("shutdown requested, saving state and exiting")
			if err := st.Save(cfg.StateFile); err != nil {
				log.Error("state save on exit failed", "err", err)
			}
			return nil
		case <-ticker.C:
			if err := runPass(rootCtx, log, runner, st, cfg.StateFile); err != nil {
				log.Error("sync pass failed", "err", err)
			}
		}
	}
}

func runPass(ctx context.Context, log *slog.Logger, r *syncpkg.Runner, st *state.State, stateFile string) error {
	start := time.Now()
	n, err := r.RunOnce(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Error("run once", "err", err)
	}
	if err := st.Save(stateFile); err != nil {
		log.Error("state save failed", "err", err)
		return err
	}
	log.Info("pass complete", "processed", n, "elapsed", time.Since(start))
	return nil
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lv})
	return slog.New(h)
}
