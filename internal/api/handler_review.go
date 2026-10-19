// Package api — handler_review.go
//
// Reviewer workflow for completed experiments:
//
//	GET  /api/v1/reviews/queue          pending (completed, unreviewed) pairs
//	POST /api/v1/experiments/{id}/review  record a decision
//	GET  /api/v1/experiments/{id}/review  read the recorded decision
//
// Decisions are append-only in spirit: a pair can be reviewed once. Changing a
// verdict requires a new pair, which keeps the audit trail honest.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/yourorg/financial-crime-red-team/internal/audit"
	"github.com/yourorg/financial-crime-red-team/internal/experiment"
)

// ReviewHandler serves the experiment review queue.
type ReviewHandler struct {
	expRepo   *experiment.Repository
	auditRepo *audit.Repository
}

// NewReviewHandler creates a ReviewHandler.
func NewReviewHandler(expRepo *experiment.Repository, auditRepo *audit.Repository) *ReviewHandler {
	return &ReviewHandler{expRepo: expRepo, auditRepo: auditRepo}
}

type reviewRequest struct {
	Decision  experiment.ReviewDecisionValue `json:"decision"`
	Rationale string                         `json:"rationale"`
}

// Queue returns completed experiments that have not been reviewed.
func (h *ReviewHandler) Queue(w http.ResponseWriter, r *http.Request) {
	items, err := h.expRepo.ListReviewQueue(r.Context(), 100)
	if err != nil {
		slog.Error("list review queue", "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to load review queue")
		return
	}
	type reviewQueueItem struct {
		experiment.Pair
		EvasionDelta     *int  `json:"evasion_delta,omitempty"`
		OutcomeUnchanged *bool `json:"outcome_unchanged,omitempty"`
	}
	out := make([]reviewQueueItem, 0, len(items))
	for _, it := range items {
		item := reviewQueueItem{Pair: it.Pair}
		if it.Outcome != nil {
			delta := it.Outcome.EvasionDelta
			unchanged := it.Outcome.OutcomeUnchanged
			item.EvasionDelta = &delta
			item.OutcomeUnchanged = &unchanged
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"queue": out, "count": len(out)})
}

// CreateReview records a decision on a completed experiment.
func (h *ReviewHandler) CreateReview(w http.ResponseWriter, r *http.Request) {
	pairID, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req reviewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Request body must be valid JSON")
		return
	}
	if !req.Decision.Valid() {
		writeError(w, http.StatusUnprocessableEntity, "validation_error",
			"decision must be approved, rejected or escalated")
		return
	}
	if req.Rationale == "" {
		writeError(w, http.StatusUnprocessableEntity, "validation_error",
			"rationale is required so the decision is explainable")
		return
	}

	pair, err := h.expRepo.GetPair(r.Context(), pairID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "not_found", "Experiment not found")
			return
		}
		slog.Error("get pair for review", "pair_id", pairID, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to load experiment")
		return
	}
	if pair.Status != experiment.StatusCompleted {
		writeError(w, http.StatusConflict, "experiment_not_completed",
			"Only completed experiments can be reviewed")
		return
	}

	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing_token", "Authentication is required")
		return
	}

	decision, err := h.expRepo.SaveReview(r.Context(), experiment.ReviewDecision{
		PairID:    pairID,
		Reviewer:  principal.Username,
		Decision:  req.Decision,
		Rationale: req.Rationale,
	})
	if err != nil {
		if errors.Is(err, experiment.ErrAlreadyReviewed) {
			writeError(w, http.StatusConflict, "already_reviewed",
				"This experiment already has a review decision")
			return
		}
		slog.Error("save review", "pair_id", pairID, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to save review")
		return
	}

	if h.auditRepo != nil {
		orgID, err := h.auditRepo.DefaultOrgID(r.Context())
		if err == nil {
			if err := h.auditRepo.Record(r.Context(), audit.Event{
				OrgID:      orgID,
				EventType:  audit.EventExperimentReviewed,
				ActorUID:   principal.Username,
				TargetType: "experiment",
				TargetID:   pairID,
				Details: map[string]any{
					"decision":  string(req.Decision),
					"rationale": req.Rationale,
				},
				IPAddress: r.RemoteAddr,
				UserAgent: r.UserAgent(),
			}); err != nil {
				slog.Warn("record review audit event", "pair_id", pairID, "err", err)
			}
		}
	}

	writeJSON(w, http.StatusCreated, decision)
}

// GetReview returns the recorded decision for a pair.
func (h *ReviewHandler) GetReview(w http.ResponseWriter, r *http.Request) {
	pairID, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	decision, err := h.expRepo.GetReview(r.Context(), pairID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isNotFound(err) {
			writeError(w, http.StatusNotFound, "not_reviewed", "No review decision for this experiment")
			return
		}
		slog.Error("get review", "pair_id", pairID, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to load review")
		return
	}
	writeJSON(w, http.StatusOK, decision)
}
