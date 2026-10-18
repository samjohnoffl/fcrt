// Package api — handler_cohort.go
//
// HTTP handlers for /api/v1/cohorts.
// All DB calls use parameterised queries via bank.Repository (T-03).
// No business logic lives here; this is purely HTTP ↔ domain translation.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/job"
)

// CohortHandler handles cohort CRUD endpoints.
type CohortHandler struct {
	repo    *bank.Repository
	jobRepo *job.Repository
}

// NewCohortHandler creates a CohortHandler.
func NewCohortHandler(repo *bank.Repository, jobRepo *job.Repository) *CohortHandler {
	return &CohortHandler{repo: repo, jobRepo: jobRepo}
}

// ─── GET /api/v1/cohorts ─────────────────────────────────────────────────────

// ListCohorts returns all cohorts with their manifest summaries.
func (h *CohortHandler) ListCohorts(w http.ResponseWriter, r *http.Request) {
	cohorts, err := h.repo.ListCohorts(r.Context())
	if err != nil {
		slog.Error("list cohorts", "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to list cohorts")
		return
	}
	if cohorts == nil {
		cohorts = []bank.Cohort{}
	}
	writeJSON(w, http.StatusOK, cohorts)
}

// ─── POST /api/v1/cohorts ────────────────────────────────────────────────────

type createCohortRequest struct {
	Name       string         `json:"name"`
	Seed       int64          `json:"seed"`
	Predicate  bank.Predicate `json:"predicate"`
	WindowDays int            `json:"window_days"` // defaults to 90
}

// CreateCohort inserts a new cohort and enqueues a generation job.
func (h *CohortHandler) CreateCohort(w http.ResponseWriter, r *http.Request) {
	var req createCohortRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Request body must be valid JSON")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "name is required")
		return
	}
	if req.Seed == 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "seed must be non-zero")
		return
	}
	days := req.WindowDays
	if days <= 0 {
		days = 90
	}

	now := time.Now().UTC()
	windowStart := now.AddDate(0, 0, -days)
	windowEnd := now

	c := bank.Cohort{
		ID:               uuid.New(),
		Name:             req.Name,
		Seed:             req.Seed,
		Predicate:        req.Predicate,
		GeneratorVersion: bank.GeneratorVersion,
		WindowStart:      &windowStart,
		WindowEnd:        &windowEnd,
	}

	created, err := h.repo.CreateCohort(r.Context(), c)
	if err != nil {
		slog.Error("create cohort", "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to create cohort")
		return
	}

	// Enqueue deterministic data generation (DECISION-004). The worker claims
	// the job, generates the synthetic cohort, persists it, and locks it.
	if h.jobRepo != nil {
		if _, err := h.jobRepo.Enqueue(r.Context(), job.TypeGenerateCohort, job.GenerateCohortPayload{
			CohortID:   created.ID,
			Seed:       created.Seed,
			WindowDays: days,
		}); err != nil {
			slog.Error("enqueue cohort generation", "cohort_id", created.ID, "err", err)
			w.Header().Set("X-FCRT-Warning",
				"cohort created but generation job could not be enqueued")
		}
	}

	writeJSON(w, http.StatusCreated, created)
}

// ─── GET /api/v1/cohorts/{id} ────────────────────────────────────────────────

// GetCohort returns a single cohort by ID.
func (h *CohortHandler) GetCohort(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	c, err := h.repo.GetCohort(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "not_found", "Cohort not found")
			return
		}
		slog.Error("get cohort", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get cohort")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// ─── POST /api/v1/cohorts/{id}/lock ──────────────────────────────────────────

// LockCohort marks a cohort as immutable (AC-01.2).
func (h *CohortHandler) LockCohort(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	c, err := h.repo.LockCohort(r.Context(), id)
	if err != nil {
		if errors.Is(err, bank.ErrCohortLocked) {
			writeError(w, http.StatusConflict, "already_locked", "Cohort is already locked")
			return
		}
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "not_found", "Cohort not found")
			return
		}
		slog.Error("lock cohort", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to lock cohort")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// ─── GET /api/v1/cohorts/{id}/summary ────────────────────────────────────────

// GetCohortSummary returns entity/account/transaction counts for a cohort.
// Used by the Gemini scenario prompt builder.
func (h *CohortHandler) GetCohortSummary(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	c, err := h.repo.GetCohort(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "not_found", "Cohort not found")
			return
		}
		slog.Error("get cohort summary", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get cohort")
		return
	}
	writeJSON(w, http.StatusOK, c.Manifest)
}
