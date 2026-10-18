// cmd/api/main.go — API server entry point.
//
// Wires together config, database, Firebase auth, and the chi HTTP router.
// Business logic lives exclusively in internal/* packages; this file only
// does environment bootstrapping and graceful shutdown.
//
// Environment variables (see .env.example):
//
//	DATABASE_URL  — PostgreSQL DSN (required)
//	API_PORT      — TCP port to listen on (default: 8080)
//	CORS_ORIGINS  — comma-separated allowed origins (default: http://localhost:5173)
//	FIREBASE_PROJECT — GCP project ID for Firebase Admin token verification
//	AUTH_MODE        — local (default) | firebase | none
//	AUTH_USERS       — local accounts, "username:password:role[,...]" (seeds the DB)
//	AUTH_SESSION_SECRET — HMAC secret for session tokens (required in production)
//	GEMINI_API_KEY   — Gemini API key (takes precedence; no GCP project needed)
//	VERTEX_PROJECT   — GCP project ID for Vertex AI / Gemini (fallback if no API key)
//	GEMINI_MODEL     — model override (default: scenario.DefaultModel)
//	LOG_LEVEL        — debug|info|warn|error (default: info)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/financial-crime-red-team/internal/api"
	"github.com/yourorg/financial-crime-red-team/internal/audit"
	"github.com/yourorg/financial-crime-red-team/internal/auth"
	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/coverage"
	"github.com/yourorg/financial-crime-red-team/internal/discovery"
	"github.com/yourorg/financial-crime-red-team/internal/evidence"
	"github.com/yourorg/financial-crime-red-team/internal/experiment"
	"github.com/yourorg/financial-crime-red-team/internal/job"
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

	// AUTH_MODE selects the authentication backend:
	//   local    (default) username/password -> signed session token
	//   firebase Firebase ID tokens
	//   none     no auth (LOCAL DEV ONLY)
	authMode := strings.ToLower(envOrDefault("AUTH_MODE", "local"))

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
	auditRepo := audit.NewRepository(pool)
	jobRepo := job.NewRepository(pool)
	evidenceRepo := evidence.NewRepository(pool)
	discoveryRepo := discovery.NewRepository(pool)

	// ── Authentication ───────────────────────────────────────────────────────
	var (
		verifier    api.AuthVerifier
		credentials api.Credentials // username/password directory
		userAdmin   api.UserAdmin   // user management (admin UI)
		signer      *api.SessionSigner
	)

	switch authMode {
	case "local":
		spec := os.Getenv("AUTH_USERS")
		us, err := api.NewUserStore(spec)
		if err != nil {
			slog.Error("auth: invalid AUTH_USERS", "err", err)
			os.Exit(1)
		}

		secret := os.Getenv("AUTH_SESSION_SECRET")
		if secret == "" {
			secret = "insecure-dev-session-secret-change-me"
			slog.Warn("AUTH_SESSION_SECRET not set — using an insecure development secret")
		}
		signer = api.NewSessionSigner(secret, authTTL())
		verifier = api.NewSessionVerifier(signer)

		// Preferred: the database-backed directory, which the admin user
		// management UI can edit. It is seeded from AUTH_USERS on first boot
		// and never clobbers an account that already exists.
		userRepo := auth.NewRepository(pool)
		if err := seedUsers(ctx, userRepo, us); err != nil {
			slog.Warn("auth: database user directory unavailable — falling back to AUTH_USERS",
				"err", err)
			credentials = api.NewEnvCredentials(us)
		} else {
			credentials = userRepo
			userAdmin = userRepo
		}

		if us.UsesDefaultCredentials() {
			slog.Warn("SECURITY: AUTH_MODE=local is using default test credentials " +
				"(e.g. admin/admin). Anyone who can reach this service can sign in. " +
				"Set AUTH_USERS to change them.")
		}
		slog.Info("auth enabled", "mode", "local", "session_ttl", signer.TTL().String(),
			"user_management", userAdmin != nil)

	case "firebase":
		if firebaseProject == "" {
			slog.Error("AUTH_MODE=firebase requires FIREBASE_PROJECT")
			os.Exit(1)
		}
		fv, err := api.NewFirebaseVerifier(ctx, firebaseProject)
		if err != nil {
			slog.Error("init firebase verifier", "project", firebaseProject, "err", err)
			os.Exit(1)
		}
		verifier = fv
		slog.Info("auth enabled", "mode", "firebase", "project", firebaseProject)

	case "none":
		slog.Warn("AUTH_MODE=none — ALL REQUESTS ARE TREATED AS ADMIN (local dev only)")
		verifier = api.NoopVerifier{}

	default:
		slog.Error("unknown AUTH_MODE", "mode", authMode, "want", "local|firebase|none")
		os.Exit(1)
	}

	// ── Gemini provider ──────────────────────────────────────────────────────
	// Preference order:
	//   1. GEMINI_API_KEY  → Gemini API (no GCP project/billing needed)
	//   2. VERTEX_PROJECT  → Vertex AI (ADC)
	//   3. neither         → provider is nil; POST /api/v1/scenarios returns 503
	geminiAPIKey := os.Getenv("GEMINI_API_KEY")

	var provider scenario.GeminiProvider
	switch {
	case geminiAPIKey != "":
		cfg := scenario.DefaultClientConfig("", "")
		cfg.ModelID = envOrDefault("GEMINI_MODEL", scenario.DefaultModel)
		gp, err := scenario.NewGeminiAPIClient(geminiAPIKey, cfg)
		if err != nil {
			slog.Error("init gemini provider", "mode", "api-key", "err", err)
			os.Exit(1)
		}
		provider = gp
		slog.Info("gemini provider enabled", "mode", "gemini-api", "model", cfg.ModelID)

	case vertexProject != "":
		vertexLocation := envOrDefault("VERTEX_LOCATION", "us-central1")
		cfg := scenario.DefaultClientConfig(vertexProject, vertexLocation)
		cfg.ModelID = envOrDefault("GEMINI_MODEL", scenario.DefaultModel)
		gp, err := scenario.NewVertexClient(cfg)
		if err != nil {
			slog.Error("init gemini provider", "project", vertexProject, "err", err)
			os.Exit(1)
		}
		provider = gp
		slog.Info("gemini provider enabled", "mode", "vertex", "project", vertexProject, "location", vertexLocation, "model", cfg.ModelID)

	default:
		slog.Warn("no GEMINI_API_KEY or VERTEX_PROJECT set — POST /scenarios will return 503")
	}

	// ── HTTP router ──────────────────────────────────────────────────────────
	router := api.NewRouter(api.RouterConfig{
		Verifier:       verifier,
		Credentials:    credentials,
		UserAdmin:      userAdmin,
		Signer:         signer,
		BankRepo:       bankRepo,
		ScenarioRepo:   scenarioRepo,
		ExperimentRepo: experimentRepo,
		CoverageRepo:   coverageRepo,
		AuditRepo:      auditRepo,
		JobRepo:        jobRepo,
		EvidenceRepo:   evidenceRepo,
		DiscoveryRepo:  discoveryRepo,
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

// authTTL parses AUTH_SESSION_TTL_HOURS (default 12h).
func authTTL() time.Duration {
	raw := os.Getenv("AUTH_SESSION_TTL_HOURS")
	if raw == "" {
		return 12 * time.Hour
	}
	hours, err := strconv.Atoi(raw)
	if err != nil || hours <= 0 {
		slog.Warn("invalid AUTH_SESSION_TTL_HOURS — using 12h", "value", raw)
		return 12 * time.Hour
	}
	return time.Duration(hours) * time.Hour
}

// seedUsers seeds the database user directory from the AUTH_USERS spec. It
// inserts only accounts that do not exist yet, so admin edits survive restarts.
func seedUsers(ctx context.Context, repo *auth.Repository, store *api.UserStore) error {
	users := store.All()
	for _, u := range users {
		if err := repo.EnsureUser(ctx, u.Username, u.Password, string(u.Role)); err != nil {
			return fmt.Errorf("seed %q: %w", u.Username, err)
		}
	}
	n, err := repo.Count(ctx)
	if err != nil {
		return err
	}
	slog.Info("auth: user directory ready", "accounts", n, "seeded_from_env", len(users))
	return nil
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required env var not set", "key", key)
		os.Exit(1)
	}
	return v
}
