package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bjl13/open-cognition/internal/api"
	"github.com/bjl13/open-cognition/internal/db"
	"github.com/bjl13/open-cognition/internal/storage"
)

func main() {
	// --- Configuration ---
	dbDSN := env("DATABASE_URL", "postgres://cognition:cognition@localhost:5432/cognition?sslmode=disable")
	port := env("PORT", "8080")

	storeCfg := storage.Config{
		Endpoint:        env("STORAGE_ENDPOINT", "http://localhost:9000"),
		Bucket:          env("STORAGE_BUCKET", "cognition"),
		AccessKeyID:     env("STORAGE_ACCESS_KEY_ID", "minioadmin"),
		SecretAccessKey: env("STORAGE_SECRET_ACCESS_KEY", "minioadmin"),
		Region:          env("STORAGE_REGION", "us-east-1"),
	}

	// --- Startup ---
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Database
	database, err := db.New(ctx, dbDSN)
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	slog.Info("database connected")

	// Object storage
	store, err := storage.New(storeCfg)
	if err != nil {
		slog.Error("storage configuration invalid", "error", err)
		os.Exit(1)
	}
	if err := store.EnsureBucket(ctx); err != nil {
		slog.Error("storage bucket unavailable", "bucket", storeCfg.Bucket, "error", err)
		os.Exit(1)
	}
	slog.Info("object storage ready", "bucket", storeCfg.Bucket)

	// HTTP server
	mux := http.NewServeMux()
	api.NewHandler(database, store).RegisterRoutes(mux)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second, // longer write timeout to allow storage uploads
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		slog.Info("shutting down gracefully")
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutCancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			slog.Error("shutdown error", "error", err)
		}
	}()

	slog.Info("control plane listening", "port", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
