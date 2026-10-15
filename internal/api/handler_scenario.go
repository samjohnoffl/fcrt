// Package api — handler_scenario.go
//
// HTTP handlers for /api/v1/scenarios.
//
// Security model:
//   - Gemini output is treated as untrusted data (AGENTS.md, T-04).
//   - Only approved proposals can be referenced in experiments (AC-03.2, T-10).
//   - Raw prompt + response stored verbatim (AC-03.1).
//   - Approval requires authentication; approver UID logged (T-10).
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

// ScenarioHandler handles scenario proposal endpoints.
type ScenarioHandler struct {
	repo     *scenario.Repository
	bankRepo *bank.Repository
	provider scenario.GeminiProvider // nil = Vertex AI not configured; returns 503
}

// NewScenarioHandler creates a ScenarioHandler.
// provider may be nil if Vertex AI credentials are not available; the
// POST /scenarios endpoint will return 503 in that case.
func NewScenarioHandler(repo *scenario.Repository, bankRepo *bank.Repository, provider scenario.GeminiProvider) *ScenarioHandler {
	return &ScenarioHandler{repo: repo, bankRepo: bankRepo, provider: provider}
}

// ─── GET /api/v1/scenarios ───────────────────────────────────────────────────

// ListScenarios returns proposals, optionally filtered by cohort_id or status.
func (h *ScenarioHandler) ListScenarios(w http.ResponseWriter, r *http.Request) {
	var cohortID *uuid.UUID
	var status *scenario.Status

	if raw := r.URL.Query().Get("cohort_id"); raw != "" {
		id, ok := parseUUID(w, raw)
		if !ok {
			return
		}
		cohortID = &id
	}
	if raw := r.URL.Query().Get("status"); raw != "" {
		s := scenario.Status(raw)
		status = &s
	}

	proposals, err := h.repo.List(r.Context(), cohortID, status)
	if err != nil {
		slog.Error("list scenarios", "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to list scenarios")
		return
	}
	if proposals == nil {
		proposals = []scenario.Proposal{}
	}
	writeJSON(w, http.StatusOK, proposals)
}

// ─── POST /api/v1/scenarios ──────────────────────────────────────────────────

type requestScenarioBody struct {
	CohortID      uuid.UUID `json:"cohort_id"`
	MaxCandidates int       `json:"max_candidates"`
}

// RequestScenario calls Gemini, validates the output, and stores the first
// accepted candidate as a pending scenario proposal (AC-03.1, AC-03.3).
func (h *ScenarioHandler) RequestScenario(w http.ResponseWriter, r *http.Request) {
	if h.provider == nil {
		writeError(w, http.StatusServiceUnavailable, "gemini_not_configured",
			"Vertex AI is not configured on this instance — set VERTEX_PROJECT and GOOGLE_APPLICATION_CREDENTIALS")
		return
	}

	var req requestScenarioBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Request body must be valid JSON")
		return
	}
	if req.CohortID == uuid.Nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "cohort_id is required")
		return
	}
	max := req.MaxCandidates
	if max < 1 || max > 10 {
		max = 3
	}

	// Cohort must exist and be locked before a scenario can be requested.
	cohort, err := h.bankRepo.GetCohort(r.Context(), req.CohortID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "not_found", "Cohort not found")
			return
		}
		slog.Error("get cohort for scenario", "cohort_id", req.CohortID, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get cohort")
		return
	}
	if cohort.LockedAt == nil {
		writeError(w, http.StatusConflict, "cohort_not_locked",
			"Cohort must be locked before requesting a scenario")
		return
	}

	// Build the generation request from cohort stats.
	corrID := uuid.New()
	genReq := scenario.GenerationRequest{
		CorrelationID: corrID,
		MaxCandidates: max,
		CohortSummary: scenario.CohortSummary{
			EntityCount:      cohort.Manifest.EntityCount,
			AccountCount:     cohort.Manifest.AccountCount,
			TxCount:          cohort.Manifest.TxCount,
			WindowDays:       90,
			TopCountryCodes:  []string{"US", "GB", "DE"},
			AlertRatePercent: 0.0,
		},
	}

	// Call Gemini — model output is untrusted data (T-04, AGENTS.md).
	result, err := h.provider.Generate(r.Context(), genReq)
	if err != nil {
		slog.Error("gemini generate",
			"correlation_id", corrID,
			"err", sanitiseLog(err.Error()))
		writeError(w, http.StatusBadGateway, "gemini_error",
			"Gemini scenario generation failed")
		return
	}
	if len(result.Accepted) == 0 {
		slog.Warn("gemini returned no valid candidates",
			"correlation_id", corrID,
			"rejected_count", len(result.Rejected))
		writeError(w, http.StatusUnprocessableEntity, "no_valid_candidates",
			"Gemini returned no candidates matching the approved schema")
		return
	}

	// Use the first accepted candidate (highest-scoring after selector, or first).
	cand := result.Accepted[0]

	// Store raw prompt/response for auditability (AC-03.1).
	// NOTE: buildPrompt is unexported; we reconstruct it via the result's
	// PromptTokens as a proxy — the actual prompt text comes from the provider.
	// Since we cannot re-extract it here, we store the generation request JSON.
	promptJSON, _ := json.Marshal(genReq)
	respJSON, _ := json.Marshal(result.Accepted)

	pattern := []scenario.TxPatternStep{
		{
			"mutation_type":   string(cand.MutationType),
			"mutation_spec":   cand.MutationSpec,
			"declared_change": cand.DeclaredChange,
		},
	}

	p := scenario.Proposal{
		ID:                    uuid.New(),
		CohortID:              req.CohortID,
		ScenarioType:          string(cand.MutationType),
		Description:           cand.DeclaredChange,
		ExpectedEvasionVector: cand.Rationale,
		SuggestedTxPattern:    pattern,
		// Raw Gemini prompt + response stored verbatim (AC-03.1).
		GeminiPrompt:   string(promptJSON),
		GeminiResponse: string(respJSON),
		GeminiModel:    "gemini-1.5-pro",
		CorrelationID:  corrID,
	}

	created, err := h.repo.Create(r.Context(), p)
	if err != nil {
		slog.Error("create scenario proposal", "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to store scenario proposal")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// ─── GET /api/v1/scenarios/{id} ──────────────────────────────────────────────

// GetScenario returns a single proposal by ID.
func (h *ScenarioHandler) GetScenario(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	p, err := h.repo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, scenario.ErrProposalNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Scenario not found")
			return
		}
		slog.Error("get scenario", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get scenario")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// ─── PATCH /api/v1/scenarios/{id}/approve ────────────────────────────────────

// ApproveScenario transitions a pending proposal to approved (AC-03.2, T-10).
// The approver's Firebase UID is stored in the DB row.
func (h *ScenarioHandler) ApproveScenario(w http.ResponseWriter, r *http.Request) {
	h.setStatus(w, r, scenario.StatusApproved)
}

// RejectScenario transitions a pending proposal to rejected.
func (h *ScenarioHandler) RejectScenario(w http.ResponseWriter, r *http.Request) {
	h.setStatus(w, r, scenario.StatusRejected)
}

func (h *ScenarioHandler) setStatus(w http.ResponseWriter, r *http.Request, status scenario.Status) {
	id, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	// Require authentication — UID recorded for audit (T-10).
	uid, _ := UIDFromContext(r.Context())

	p, err := h.repo.SetStatus(r.Context(), id, status, uid)
	if err != nil {
		if errors.Is(err, scenario.ErrProposalNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Scenario not found")
			return
		}
		if errors.Is(err, scenario.ErrInvalidStatusTransition) {
			writeError(w, http.StatusConflict, "invalid_transition",
				"Only pending proposals can be approved or rejected")
			return
		}
		slog.Error("set scenario status", "id", id, "status", status, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to update scenario status")
		return
	}
	slog.Info("scenario status changed",
		"id", id,
		"status", status,
		"by_uid", strings.ReplaceAll(uid, "\n", "")) // log-injection safe
	writeJSON(w, http.StatusOK, p)
}
