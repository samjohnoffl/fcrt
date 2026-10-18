// Package api — handler_campaign.go
//
// HTTP handlers for /api/v1/campaigns.
// Campaigns organize experiments under a shared budget and goal.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
	"github.com/yourorg/financial-crime-red-team/internal/coverage"
	"github.com/yourorg/financial-crime-red-team/internal/experiment"
)

// CampaignHandler handles campaign endpoints.
type CampaignHandler struct {
	expRepo      *experiment.Repository
	coverageRepo *coverage.Repository
}

// NewCampaignHandler creates a CampaignHandler.
func NewCampaignHandler(expRepo *experiment.Repository, coverageRepo *coverage.Repository) *CampaignHandler {
	return &CampaignHandler{expRepo: expRepo, coverageRepo: coverageRepo}
}

// RegisterCampaignRoutes registers campaign endpoints on the router.
func (h *CampaignHandler) RegisterRoutes(r chi.Router) {
	r.Get("/campaigns", h.ListCampaigns)
	r.Post("/campaigns", h.CreateCampaign)
	r.Get("/campaigns/{id}", h.GetCampaign)
	r.Get("/campaigns/{id}/coverage", h.GetCampaignCoverage)
	r.Get("/campaigns/{id}/benchmark", h.GetCampaignBenchmark)
}

// ─── Types ─────────────────────────────────────────────────────────────────────

type campaignStatus string

const (
	CampaignStatusActive    campaignStatus = "active"
	CampaignStatusCompleted campaignStatus = "completed"
	CampaignStatusPaused    campaignStatus = "paused"
)

// Campaign is a high-level grouping of experiments under a shared goal.
type Campaign struct {
	ID              uuid.UUID          `json:"id"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	Budget          int                `json:"budget"` // max experiments
	ExperimentsUsed int                `json:"experiments_used"`
	Status          campaignStatus     `json:"status"`
	ControlConfig   *aml.ControlConfig `json:"control_config,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type createCampaignRequest struct {
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	Budget        int                `json:"budget"`
	ControlConfig *aml.ControlConfig `json:"control_config,omitempty"`
}

type updateCampaignRequest struct {
	Description *string         `json:"description"`
	Budget      *int            `json:"budget"`
	Status      *campaignStatus `json:"status"`
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
		// Use default AML config
		cfg := aml.DefaultControlConfig()
		req.ControlConfig = cfg
	} else {
		if err := req.ControlConfig.Validate(); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "validation_error", "invalid control config: "+err.Error())
			return
		}
	}

	// In a full implementation, we'd persist this to a campaigns table.
	// For now, return a mock campaign with the request data.
	campaign := Campaign{
		ID:              uuid.New(),
		Name:            req.Name,
		Description:     req.Description,
		Budget:          req.Budget,
		ExperimentsUsed: 0,
		Status:          CampaignStatusActive,
		ControlConfig:   req.ControlConfig,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}

	slog.Info("campaign created", "campaign_id", campaign.ID, "name", campaign.Name)
	writeJSON(w, http.StatusCreated, campaign)
}

// ─── GET /api/v1/campaigns ────────────────────────────────────────────────────

func (h *CampaignHandler) ListCampaigns(w http.ResponseWriter, r *http.Request) {
	// In a full implementation, query from campaigns table.
	// For now, return empty list.
	writeJSON(w, http.StatusOK, []Campaign{})
}

// ─── GET /api/v1/campaigns/{id} ────────────────────────────────────────────────

func (h *CampaignHandler) GetCampaign(w http.ResponseWriter, r *http.Request) {
	_, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	// In a full implementation, query from campaigns table.
	writeError(w, http.StatusNotFound, "not_found", "Campaign not found")
}

// ─── GET /api/v1/campaigns/{id}/coverage ───────────────────────────────────────

func (h *CampaignHandler) GetCampaignCoverage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	// In a full implementation, aggregate coverage from all experiments in campaign.
	writeJSON(w, http.StatusOK, map[string]any{
		"campaign_id": id,
		"coverage": map[string]any{
			"scenario": map[string]any{},
			"control":  map[string]any{},
			"outcome":  map[string]any{},
		},
		"note": "Full implementation requires campaign-experiment linkage",
	})
}

// ─── GET /api/v1/campaigns/{id}/benchmark ──────────────────────────────────────

// GetCampaignBenchmark is retained as an explicit, honest failure.
//
// Campaigns are not persisted (see handler_campaign.go CreateCampaign), so a
// campaign ID cannot be resolved to a cohort or to stored runs. Returning a
// made-up comparison here was the old behaviour and is deliberately removed.
// Use GET /api/v1/cohorts/{id}/benchmark instead.
func (h *CampaignHandler) GetCampaignBenchmark(w http.ResponseWriter, r *http.Request) {
	if _, ok := parseUUID(w, chi.URLParam(r, "id")); !ok {
		return
	}
	writeError(w, http.StatusNotFound, "campaign_not_persisted",
		"Campaigns are not persisted; use GET /api/v1/cohorts/{id}/benchmark with a real cohort ID")
}
