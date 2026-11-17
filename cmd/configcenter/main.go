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

	"github.com/OXeu/configcenter/internal/config"
	"github.com/OXeu/configcenter/internal/server"
	"github.com/OXeu/configcenter/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("configcenter stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, st, err := loadConfiguration(ctx, os.LookupEnv)
	if err != nil {
		return err
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("close database", "error", err)
		}
	}()
	logger.Info("configuration loaded", "addr", cfg.Addr, "public_url", cfg.PublicURL, "oauth", cfg.OAuth.Enabled(), "database_driver", cfg.DatabaseDriver)

	engine, err := server.Engine(ctx, cfg, st, logger)
	if err != nil {
		return fmt.Errorf("build server: %w", err)
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr, "oauth", cfg.OAuth.Enabled())
		errc <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case serveErr := <-errc:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", serveErr)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func loadConfiguration(ctx context.Context, lookup func(string) (string, bool)) (config.Config, *store.Store, error) {
	loader, err := config.Prepare(ctx, lookup)
	if err != nil {
		return config.Config{}, nil, fmt.Errorf("load bootstrap configuration: %w", err)
	}
	st, err := store.OpenDatabase(ctx, loader.DatabaseDriver, loader.DatabaseDSN, loader.Bootstrap.AdminEmail, loader.Bootstrap.AdminName)
	if err != nil {
		return config.Config{}, nil, fmt.Errorf("open database: %w", err)
	}
	cfg, err := loader.Load(ctx, st.ConfigurationSource("configcenter", "production"))
	if err != nil {
		return config.Config{}, nil, fmt.Errorf("load runtime configuration: %w", errors.Join(err, st.Close()))
	}
	return cfg, st, nil
}
