// Package api — router.go
//
// Assembles the chi router and registers all route groups.
// The AuthMiddleware wraps every /api/v1/* route — no handler bypass possible.
package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/coverage"
	"github.com/yourorg/financial-crime-red-team/internal/experiment"
	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

// RouterConfig holds all dependencies needed to assemble the router.
type RouterConfig struct {
	Verifier     AuthVerifier
	BankRepo     *bank.Repository
	ScenarioRepo *scenario.Repository
	ExperimentRepo *experiment.Repository
	CoverageRepo *coverage.Repository
	Provider     scenario.GeminiProvider // nil = Vertex AI not configured
	CORSOrigins  []string
}

// NewRouter builds and returns the fully-configured chi router.
func NewRouter(cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

	// Global middleware: logging, panic recovery.
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware(cfg.CORSOrigins))

	// Health check (unauthenticated — used by Cloud Run health probe).
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Instantiate handlers.
	cohortH := NewCohortHandler(cfg.BankRepo)
	scenarioH := NewScenarioHandler(cfg.ScenarioRepo, cfg.BankRepo, cfg.Provider)
	experimentH := NewExperimentHandler(cfg.ExperimentRepo, cfg.BankRepo, cfg.ScenarioRepo, cfg.CoverageRepo)
	coverageH := NewCoverageHandler(cfg.CoverageRepo)

	// /api/v1 — all routes require authentication (AC-07.1, T-01).
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(AuthMiddleware(cfg.Verifier))

		// Cohorts
		r.Get("/cohorts", cohortH.ListCohorts)
		r.Post("/cohorts", cohortH.CreateCohort)
		r.Get("/cohorts/{id}", cohortH.GetCohort)
		r.Get("/cohorts/{id}/summary", cohortH.GetCohortSummary)
		r.Post("/cohorts/{id}/lock", cohortH.LockCohort)

		// Scenarios (AC-03, T-10)
		r.Get("/scenarios", scenarioH.ListScenarios)
		r.Post("/scenarios", scenarioH.RequestScenario)
		r.Get("/scenarios/{id}", scenarioH.GetScenario)
		r.Patch("/scenarios/{id}/approve", scenarioH.ApproveScenario)
		r.Patch("/scenarios/{id}/reject", scenarioH.RejectScenario)

		// Experiments (AC-04, AC-06)
		r.Get("/experiments", experimentH.ListExperiments)
		r.Post("/experiments", experimentH.CreateExperiment)
		r.Get("/experiments/{id}", experimentH.GetExperiment)
		r.Get("/experiments/{id}/evidence", experimentH.GetEvidence)
		r.Post("/experiments/{id}/replay", experimentH.ReplayExperiment)

		// Coverage (AC-05)
		r.Get("/coverage", coverageH.GetCoverage)
	})

	return r
}

// ─── CORS middleware ──────────────────────────────────────────────────────────

// corsMiddleware handles CORS headers for the allowed origins.
// Only origins in the allowlist get Access-Control-Allow-Origin.
func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	originSet := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[strings.TrimRight(o, "/")] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && originSet[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ─── Shared handler helpers ───────────────────────────────────────────────────

// parseUUID parses a UUID from a URL parameter.
// Writes a 400 response and returns false if the parameter is invalid.
func parseUUID(w http.ResponseWriter, raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id",
			fmt.Sprintf("'%s' is not a valid UUID", raw))
		return uuid.Nil, false
	}
	return id, true
}

// isNotFound returns true for "not found" errors from pgx or our repositories.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no rows")
}
