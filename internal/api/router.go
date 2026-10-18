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

	"github.com/yourorg/financial-crime-red-team/internal/audit"
	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/coverage"
	"github.com/yourorg/financial-crime-red-team/internal/discovery"
	"github.com/yourorg/financial-crime-red-team/internal/evidence"
	"github.com/yourorg/financial-crime-red-team/internal/experiment"
	"github.com/yourorg/financial-crime-red-team/internal/job"
	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

// RouterConfig holds all dependencies needed to assemble the router.
type RouterConfig struct {
	Verifier       AuthVerifier
	Credentials    Credentials    // username/password directory (AUTH_MODE=local)
	UserAdmin      UserAdmin      // user management (nil = endpoints return 501)
	Signer         *SessionSigner // session token signer (AUTH_MODE=local)
	BankRepo       *bank.Repository
	ScenarioRepo   *scenario.Repository
	ExperimentRepo *experiment.Repository
	CoverageRepo   *coverage.Repository
	AuditRepo      *audit.Repository
	JobRepo        *job.Repository
	EvidenceRepo   *evidence.Repository
	DiscoveryRepo  *discovery.Repository
	Provider       scenario.GeminiProvider // nil = Vertex AI not configured
	CORSOrigins    []string
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
	cohortH := NewCohortHandler(cfg.BankRepo, cfg.JobRepo)
	scenarioH := NewScenarioHandler(cfg.ScenarioRepo, cfg.BankRepo, cfg.AuditRepo, cfg.Provider)
	experimentH := NewExperimentHandler(cfg.ExperimentRepo, cfg.BankRepo, cfg.ScenarioRepo, cfg.CoverageRepo, cfg.EvidenceRepo)
	coverageH := NewCoverageHandler(cfg.CoverageRepo)
	discoveryH := NewDiscoveryHandler(cfg.DiscoveryRepo, cfg.BankRepo)
	auditH := NewAuditHandler(cfg.AuditRepo)
	campaignH := NewCampaignHandler(cfg.ExperimentRepo, cfg.CoverageRepo)
	regressionH := NewRegressionHandler(cfg.ExperimentRepo, cfg.BankRepo)
	authH := NewAuthHandler(cfg.Credentials, cfg.Signer, cfg.AuditRepo)
	usersH := NewUserHandler(cfg.UserAdmin, cfg.AuditRepo)

	// /api/v1 — authentication + role-based authorization (AC-07, T-01).
	//
	// Policy: every authenticated route requires at least `analyst`; the audit
	// log and user management additionally require `admin`.
	r.Route("/api/v1", func(r chi.Router) {
		// Public: obtain a session token.
		r.Post("/auth/login", authH.Login)

		r.Group(func(r chi.Router) {
			r.Use(AuthMiddleware(cfg.Verifier))
			r.Use(RequireRole(RoleAnalyst))

			// Session introspection.
			r.Get("/auth/me", authH.Me)
			r.Post("/auth/logout", authH.Logout)

			// Cohorts.
			r.Get("/cohorts", cohortH.ListCohorts)
			r.Post("/cohorts", cohortH.CreateCohort)
			r.Get("/cohorts/{id}", cohortH.GetCohort)
			r.Get("/cohorts/{id}/summary", cohortH.GetCohortSummary)
			r.Post("/cohorts/{id}/lock", cohortH.LockCohort)

			// Scenarios (AC-03, T-10).
			r.Get("/scenarios", scenarioH.ListScenarios)
			r.Post("/scenarios", scenarioH.RequestScenario)
			r.Get("/scenarios/{id}", scenarioH.GetScenario)
			r.Patch("/scenarios/{id}/approve", scenarioH.ApproveScenario)
			r.Patch("/scenarios/{id}/reject", scenarioH.RejectScenario)

			// Experiments (AC-04, AC-06).
			r.Get("/experiments", experimentH.ListExperiments)
			r.Post("/experiments", experimentH.CreateExperiment)
			r.Get("/experiments/{id}", experimentH.GetExperiment)
			r.Get("/experiments/{id}/evidence", experimentH.GetEvidence)
			r.Post("/experiments/{id}/replay", experimentH.ReplayExperiment)

			// Coverage (AC-05).
			r.Get("/coverage", coverageH.GetCoverage)

			// Adaptive discovery: choose the next candidate with a persisted,
			// explainable score. See docs/DECISIONS.md (DECISION-016).
			r.Post("/cohorts/{id}/discovery", discoveryH.Run)
			r.Get("/cohorts/{id}/discovery", discoveryH.Latest)
			r.Get("/cohorts/{id}/benchmark", discoveryH.Benchmark)

			// Campaigns.
			r.Get("/campaigns", campaignH.ListCampaigns)
			r.Post("/campaigns", campaignH.CreateCampaign)
			r.Get("/campaigns/{id}", campaignH.GetCampaign)
			r.Get("/campaigns/{id}/coverage", campaignH.GetCampaignCoverage)
			r.Get("/campaigns/{id}/benchmark", campaignH.GetCampaignBenchmark)

			// Regression Test Pack (PRODUCT_SPEC item 7).
			r.Get("/regression/pack", regressionH.GetPack)
			r.Post("/regression/verify", regressionH.VerifyPack)

			// ── Admin-only ──────────────────────────────────────────────────
			r.With(RequireRole(RoleAdmin)).Get("/audit", auditH.ListAuditEvents)
			r.With(RequireRole(RoleAdmin)).Get("/users", usersH.List)
			r.With(RequireRole(RoleAdmin)).Post("/users", usersH.Create)
			r.With(RequireRole(RoleAdmin)).Patch("/users/{username}", usersH.Update)
			r.With(RequireRole(RoleAdmin)).Delete("/users/{username}", usersH.Delete)
		})
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
