// Command api serves the devices REST API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gtalha07/api-device-management/internal/database"
	"github.com/gtalha07/api-device-management/internal/device"
	"github.com/gtalha07/api-device-management/internal/notify"
	"github.com/gtalha07/api-device-management/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger); err != nil {
		logger.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

// run wires the application together and serves until ctx is cancelled.
func run(ctx context.Context, logger *slog.Logger) error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	// database
	pool, err := database.Connect(ctx, dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := database.Migrate(pool, migrations.FS); err != nil {
		return err
	}
	logger.Info("database ready")

	// wiring the pieces together
	repo := device.NewPostgresRepository(pool)
	notifier := notify.NewLogNotifier(logger)
	service := device.NewService(repo, notifier, logger)

	mux := http.NewServeMux()
	device.NewHandler(service, logger).Register(mux)

	// health check
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(pingCtx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// server with timeouts
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	// serving and graceful shutdown
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	logger.Info("stopped")
	return nil
}
