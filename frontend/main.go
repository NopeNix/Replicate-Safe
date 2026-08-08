// Command frontend serves the replicate-safe frontend: a read-only web UI
// for browsing predictions and previewing their output files.
//
// It reads from the same on-disk directory that replicate-safe writes to.
package main

import (
	"context"
	"embed"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/phil/Replicate-Safe/frontend/internal/server"
)

//go:embed all:web
var webFS embed.FS

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	dataDir := getenv("DATA_DIR", "/data")
	addr := getenv("LISTEN_ADDR", ":8080")
	cacheSecs, _ := strconv.Atoi(getenv("CACHE_TTL", "5"))
	cacheTTL := time.Duration(cacheSecs) * time.Second

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	log.Info("starting replicate-safe-frontend",
		"data_dir", dataDir,
		"listen", addr,
		"cache_ttl", cacheTTL,
	)

	srv := &server.Server{
		DataDir:  dataDir,
		WebFS:    webFS,
		Addr:     addr,
		CacheTTL: cacheTTL,
		Log:      log,
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	rootCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-rootCtx.Done()
		log.Info("shutdown requested")
		shutCtx, sc := context.WithTimeout(context.Background(), 10*time.Second)
		defer sc()
		_ = httpSrv.Shutdown(shutCtx)
	}()

	log.Info("listening", "addr", addr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
