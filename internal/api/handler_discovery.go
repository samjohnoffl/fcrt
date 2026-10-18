// Package api — handler_discovery.go
//
// HTTP surface for the adaptive discovery loop. The endpoint is deliberately
// explicit: it returns the full scoring trace, not just the chosen candidate,
// because the project's claim is that selection is explainable.
package api

import (
	"encoding/json"
	"errors"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/benchmark"
	"github.com/yourorg/financial-crime-red-team/internal/discovery"
)

// DiscoveryHandler handles adaptive selection endpoints.
type DiscoveryHandler struct {
	repo     *discovery.Repository
	bankRepo *bank.Repository
}

// NewDiscoveryHandler creates a DiscoveryHandler.
func NewDiscoveryHandler(repo *discovery.Repository, bankRepo *bank.Repository) *DiscoveryHandler {
	return &DiscoveryHandler{repo: repo, bankRepo: bankRepo}
}

type runDiscoveryRequest struct {
	// Strategy is one of adaptive (default), random, gemini_only.
	Strategy string `json:"strategy"`
	// Budget is the number of candidates to select. Defaults to 5.
	Budget int `json:"budget"`
	// Seed makes the random strategy reproducible. Defaults to a value derived
	// from the cohort ID so repeated calls choose the same candidates.
	Seed *int64 `json:"seed"`
}

// Run selects the next candidates for a cohort and persists the decision.
//
// POST /api/v1/cohorts/{id}/discovery
func (h *DiscoveryHandler) Run(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "Discovery is not configured")
		return
	}
	cohortID, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, err := h.bankRepo.GetCohort(r.Context(), cohortID); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "not_found", "Cohort not found")
			return
		}
		slog.Error("get cohort for discovery", "cohort_id", cohortID, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get cohort")
		return
	}

	var req runDiscoveryRequest
	// An empty body is valid: defaults are the adaptive strategy and budget 5.
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_json", "Request body must be valid JSON")
		return
	}

	strategy := discovery.StrategyAdaptive
	if req.Strategy != "" {
		strategy = discovery.Strategy(req.Strategy)
	}
	if !strategy.Valid() {
		writeError(w, http.StatusUnprocessableEntity, "validation_error",
			"strategy must be one of: adaptive, random, gemini_only")
		return
	}

	budget := req.Budget
	if budget == 0 {
		budget = 5
	}
	if budget < 1 || budget > 50 {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "budget must be between 1 and 50")
		return
	}

	seed := deterministicSeed(cohortID)
	if req.Seed != nil {
		seed = *req.Seed
	}

	started := time.Now()
	state, err := h.repo.LoadState(r.Context(), cohortID)
	if err != nil {
		slog.Error("load discovery state", "cohort_id", cohortID, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to load discovery state")
		return
	}
	pool, err := h.repo.PendingCandidates(r.Context(), cohortID)
	if err != nil {
		slog.Error("load discovery candidates", "cohort_id", cohortID, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to load candidates")
		return
	}

	run, err := discovery.Select(cohortID, pool, state, discovery.DefaultWeights(), strategy, budget, seed)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}
	run.DurationMS = time.Since(started).Milliseconds()

	saved, err := h.repo.Save(r.Context(), run)
	if err != nil {
		slog.Error("save discovery run", "cohort_id", cohortID, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to persist discovery run")
		return
	}

	writeJSON(w, http.StatusCreated, saved)
}

// Latest returns the most recent run for each strategy, with its scoring trace.
//
// GET /api/v1/cohorts/{id}/discovery
func (h *DiscoveryHandler) Latest(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "Discovery is not configured")
		return
	}
	cohortID, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	runs, err := h.repo.LatestRuns(r.Context(), cohortID)
	if err != nil {
		slog.Error("latest discovery runs", "cohort_id", cohortID, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to load discovery runs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cohort_id": cohortID,
		"runs":      runs,
		"note": "Each run stores its strategy, weights version, seed and per-candidate " +
			"score components. Run all three strategies to enable the benchmark comparison.",
	})
}

// deterministicSeed derives a stable seed from the cohort ID. This makes a
// repeated random-strategy call select the same candidates, which is required
// for a reproducible benchmark.
func deterministicSeed(id uuid.UUID) int64 {
	h := fnv.New64a()
	_, _ = h.Write(id[:])
	return int64(h.Sum64() & 0x7fffffffffffffff)
}

// Benchmark compares the latest stored run of each strategy for a cohort.
//
// GET /api/v1/cohorts/{id}/benchmark
func (h *DiscoveryHandler) Benchmark(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "Discovery is not configured")
		return
	}
	cohortID, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, err := h.bankRepo.GetCohort(r.Context(), cohortID); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "not_found", "Cohort not found")
			return
		}
		slog.Error("get cohort for benchmark", "cohort_id", cohortID, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get cohort")
		return
	}
	result, err := benchmark.RunComparison(r.Context(), h.repo, cohortID)
	if err != nil {
		slog.Error("benchmark comparison", "cohort_id", cohortID, "err", err)
		writeError(w, http.StatusInternalServerError, "benchmark_failed", "Failed to compute benchmark")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
