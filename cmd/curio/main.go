package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"

	"curio/internal/config"
	"curio/internal/content"
	"curio/internal/httpapp"
	"curio/internal/store"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("Curio stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: c.LogLevel}))
	db, err := store.Open(c.DataDir)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	handler, err := httpapp.New(db, c, logger, version)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: ":" + c.Port, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var providers sync.WaitGroup
	if c.WikimediaEnabled {
		providers.Go(func() { content.NewWikimedia(db, logger).Run(ctx) })
	}
	if c.FeedsEnabled {
		for _, source := range content.NASAFeeds() {
			providers.Go(func() { content.FeedWorker{Source: source, Cache: db, Logger: logger}.Run(ctx) })
		}
	}
	defer func() { stop(); providers.Wait() }()
	if err := db.CleanupContent(ctx, time.Now()); err != nil {
		return fmt.Errorf("content cleanup: %w", err)
	}
	done := make(chan error, 1)
	go func() {
		logger.Info("Curio listening", "port", c.Port, "version", version)
		done <- server.ListenAndServe()
	}()
	if err := db.CleanSessions(ctx); err != nil {
		logger.Error("session cleanup failed", "error", err)
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ticker.C:
			if err := db.CleanupContent(ctx, time.Now()); err != nil {
				logger.Error("content cleanup failed", "error", err)
			}
			if err := db.CleanSessions(ctx); err != nil {
				logger.Error("session cleanup failed", "error", err)
			}
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return server.Shutdown(shutdown)
		}
	}
}
func healthcheck() error {
	port := os.Getenv("CURIO_PORT")
	if port == "" {
		port = "8080"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get("http://127.0.0.1:" + port + "/health")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("health check returned %d", res.StatusCode)
	}
	return nil
}
