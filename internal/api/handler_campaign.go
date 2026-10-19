// Package api — handler_campaign.go
//
// HTTP handlers for /api/v1/campaigns.
//
// Campaigns are persisted: a campaign names a cohort, fixes a control
// configuration and caps the number of experiments. Budget enforcement happens
// in CreateExperiment, not here, so the check and the insert use the same
// request path.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
	"github.com/yourorg/financial-crime-red-team/internal/benchmark"
	"github.com/yourorg/financial-crime-red-team/internal/campaign"
	"github.com/yourorg/financial-crime-red-team/internal/discovery"
)

// CampaignHandler handles campaign endpoints.
type CampaignHandler struct {
	repo          *campaign.Repository
	discoveryRepo *discovery.Repository
}

// NewCampaignHandler creates a CampaignHandler.
func NewCampaignHandler(repo *campaign.Repository, discoveryRepo *discovery.Repository) *CampaignHandler {
	return &CampaignHandler{repo: repo, discoveryRepo: discoveryRepo}
}

// RegisterCampaignRoutes registers campaign endpoints on the router.
func (h *CampaignHandler) RegisterRoutes(r chi.Router) {
	r.Get("/campaigns", h.ListCampaigns)
	r.Post("/campaigns", h.CreateCampaign)
	r.Get("/campaigns/{id}", h.GetCampaign)
	r.Patch("/campaigns/{id}", h.UpdateCampaign)
	r.Get("/campaigns/{id}/coverage", h.GetCampaignCoverage)
	r.Get("/campaigns/{id}/benchmark", h.GetCampaignBenchmark)
}

type createCampaignRequest struct {
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	Goal          string             `json:"goal"`
	CohortID      *uuid.UUID         `json:"cohort_id,omitempty"`
	Budget        int                `json:"budget"`
	ControlConfig *aml.ControlConfig `json:"control_config,omitempty"`
}

type updateCampaignRequest struct {
	Description *string          `json:"description"`
	Budget      *int             `json:"budget"`
	Status      *campaign.Status `json:"status"`
}

// ─── POST /api/v1/campaigns ────────────────────────────────────────────────────

func (h *CampaignHandler) CreateCampaign(w http.ResponseWriter, r *http.Request) {
	var req createCampaignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Request body must be valid JSON")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "name is required")
		return
	}
	if req.Budget <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "budget must be positive")
		return
	}
	if req.ControlConfig == nil {
		req.ControlConfig = aml.DefaultControlConfig()
	} else if err := req.ControlConfig.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "invalid control config: "+err.Error())
		return
	}

	created, err := h.repo.Create(r.Context(), campaign.Campaign{
		Name:          req.Name,
		Description:   req.Description,
		Goal:          req.Goal,
		CohortID:      req.CohortID,
		Budget:        req.Budget,
		Status:        campaign.StatusActive,
		ControlConfig: req.ControlConfig,
	})
	if err != nil {
		slog.Error("create campaign", "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to create campaign")
		return
	}
	slog.Info("campaign created", "campaign_id", created.ID, "name", created.Name)
	writeJSON(w, http.StatusCreated, created)
}

// ─── GET /api/v1/campaigns ────────────────────────────────────────────────────

func (h *CampaignHandler) ListCampaigns(w http.ResponseWriter, r *http.Request) {
	campaigns, err := h.repo.List(r.Context())
	if err != nil {
		slog.Error("list campaigns", "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to list campaigns")
		return
	}
	writeJSON(w, http.StatusOK, campaigns)
}

// ─── GET /api/v1/campaigns/{id} ────────────────────────────────────────────────

func (h *CampaignHandler) GetCampaign(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	c, err := h.repo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, campaign.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Campaign not found")
			return
		}
		slog.Error("get campaign", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get campaign")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// ─── PATCH /api/v1/campaigns/{id} ──────────────────────────────────────────────

func (h *CampaignHandler) UpdateCampaign(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req updateCampaignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Request body must be valid JSON")
		return
	}
	if req.Budget != nil && *req.Budget <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "budget must be positive")
		return
	}
	if req.Status != nil && !req.Status.Valid() {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "unknown campaign status")
		return
	}
	c, err := h.repo.Update(r.Context(), id, req.Description, req.Budget, req.Status)
	if err != nil {
		if errors.Is(err, campaign.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Campaign not found")
			return
		}
		slog.Error("update campaign", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to update campaign")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// ─── GET /api/v1/campaigns/{id}/coverage ───────────────────────────────────────

// GetCampaignCoverage returns the campaign record and its experiment count.
// Coverage maps themselves are global (rule_coverage, outcome_coverage,
// scenario_coverage); this endpoint does not pretend to slice them per
// campaign, because outcomes are not tagged with a campaign at write time.
func (h *CampaignHandler) GetCampaignCoverage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	c, err := h.repo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, campaign.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Campaign not found")
			return
		}
		slog.Error("get campaign coverage", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get campaign")
		return
	}
	used, err := h.repo.CountExperiments(r.Context(), id)
	if err != nil {
		slog.Error("count campaign experiments", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to count campaign experiments")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"campaign_id":       c.ID,
		"cohort_id":         c.CohortID,
		"experiments":       used,
		"budget":            c.Budget,
		"experiments_used":  c.ExperimentsUsed,
		"control_config":    c.ControlConfig,
		"coverage_scope":    "global",
		"coverage_endpoint": "/api/v1/coverage",
		"note": "The three coverage maps are global; campaign-level slicing is not " +
			"recorded at outcome-write time.",
	})
}

// ─── GET /api/v1/campaigns/{id}/benchmark ──────────────────────────────────────

// GetCampaignBenchmark compares strategies for the campaign's cohort using
// stored discovery runs. It returns insufficient_data honestly rather than
// inventing a winner when runs are missing.
func (h *CampaignHandler) GetCampaignBenchmark(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	c, err := h.repo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, campaign.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Campaign not found")
			return
		}
		slog.Error("get campaign for benchmark", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get campaign")
		return
	}
	if c.CohortID == nil {
		writeError(w, http.StatusUnprocessableEntity, "campaign_has_no_cohort",
			"Campaign has no cohort_id; a benchmark needs stored runs for a cohort")
		return
	}
	result, err := benchmark.RunComparison(r.Context(), h.discoveryRepo, *c.CohortID)
	if err != nil {
		slog.Error("campaign benchmark", "campaign_id", id, "cohort_id", *c.CohortID, "err", err)
		writeError(w, http.StatusInternalServerError, "benchmark_failed", "Failed to compute benchmark")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
