// cmd/api/main.go — API server entry point.
//
// Wires together config, database, Firebase auth, and the chi HTTP router.
// Business logic lives exclusively in internal/* packages; this file only
// does environment bootstrapping and graceful shutdown.
//
// Environment variables (see .env.example):
//   DATABASE_URL  — PostgreSQL DSN (required)
//   API_PORT      — TCP port to listen on (default: 8080)
//   CORS_ORIGINS  — comma-separated allowed origins (default: http://localhost:5173)
//   FIREBASE_PROJECT — GCP project ID for Firebase Admin token verification
//   VERTEX_PROJECT   — GCP project ID for Vertex AI / Gemini (optional; POST /scenarios returns 503 if unset)
//   LOG_LEVEL        — debug|info|warn|error (default: info)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/financial-crime-red-team/internal/api"
	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/coverage"
	"github.com/yourorg/financial-crime-red-team/internal/experiment"
	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

func main() {
	// ── Structured JSON logging ──────────────────────────────────────────────
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel(),
	}))
	slog.SetDefault(logger)

	// ── Config from environment ──────────────────────────────────────────────
	dbURL := mustEnv("DATABASE_URL")
	port := envOrDefault("API_PORT", "8080")
	firebaseProject := os.Getenv("FIREBASE_PROJECT")
	vertexProject := os.Getenv("VERTEX_PROJECT")
	corsOrigins := strings.Split(envOrDefault("CORS_ORIGINS", "http://localhost:5173"), ",")

	// ── Database pool ────────────────────────────────────────────────────────
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("open db pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("db ping failed", "err", err)
		os.Exit(1)
	}
	slog.Info("database connected")

	// ── Repositories ─────────────────────────────────────────────────────────
	bankRepo := bank.NewRepository(pool)
	scenarioRepo := scenario.NewRepository(pool)
	experimentRepo := experiment.NewRepository(pool)
	coverageRepo := coverage.NewRepository(pool)

	// ── Firebase auth verifier ───────────────────────────────────────────────
	// FirebaseVerifier implements api.AuthVerifier using the Firebase Admin SDK.
	// If FIREBASE_PROJECT is not set we run without auth — only acceptable in
	// local dev; Cloud Run deployments must always set FIREBASE_PROJECT.
	var verifier api.AuthVerifier
	if firebaseProject != "" {
		fv, err := api.NewFirebaseVerifier(ctx, firebaseProject)
		if err != nil {
			slog.Error("init firebase verifier", "project", firebaseProject, "err", err)
			os.Exit(1)
		}
		verifier = fv
		slog.Info("firebase auth enabled", "project", firebaseProject)
	} else {
		slog.Warn("FIREBASE_PROJECT not set — auth disabled (local dev only)")
		verifier = api.NoopVerifier{}
	}

	// ── Gemini / Vertex AI provider ──────────────────────────────────────────
	// If VERTEX_PROJECT is not set, provider is nil; POST /api/v1/scenarios
	// will return 503 with gemini_not_configured code.
	var provider scenario.GeminiProvider
	if vertexProject != "" {
		vertexLocation := envOrDefault("VERTEX_LOCATION", "us-central1")
		cfg := scenario.DefaultClientConfig(vertexProject, vertexLocation)
		gp, err := scenario.NewVertexClient(cfg)
		if err != nil {
			slog.Error("init gemini provider", "project", vertexProject, "err", err)
			os.Exit(1)
		}
		provider = gp
		slog.Info("gemini provider enabled", "project", vertexProject, "location", vertexLocation)
	} else {
		slog.Warn("VERTEX_PROJECT not set — POST /scenarios will return 503")
	}

	// ── HTTP router ──────────────────────────────────────────────────────────
	router := api.NewRouter(api.RouterConfig{
		Verifier:       verifier,
		BankRepo:       bankRepo,
		ScenarioRepo:   scenarioRepo,
		ExperimentRepo: experimentRepo,
		CoverageRepo:   coverageRepo,
		Provider:       provider,
		CORSOrigins:    corsOrigins,
	})

	// ── HTTP server ──────────────────────────────────────────────────────────
	addr := fmt.Sprintf(":%s", port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		slog.Info("api server starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-stop
	slog.Info("shutdown signal received")
	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("graceful shutdown error", "err", err)
	}
	slog.Info("server stopped")
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func logLevel() slog.Level {
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required env var not set", "key", key)
		os.Exit(1)
	}
	return v
}
