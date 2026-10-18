// Package api — handler_audit.go
//
// HTTP handler for /api/v1/audit.
// Returns audit event log (DECISION-006).
package api

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/audit"
)

// AuditHandler handles audit event endpoints.
type AuditHandler struct {
	repo *audit.Repository
}

// NewAuditHandler creates an AuditHandler.
func NewAuditHandler(repo *audit.Repository) *AuditHandler {
	return &AuditHandler{repo: repo}
}

// RegisterAuditRoutes registers audit endpoints on the router.
func (h *AuditHandler) RegisterRoutes(r chi.Router) {
	r.Get("/audit", h.ListAuditEvents)
}

// ListAuditEvents returns audit events for the current org.
// Query params: org_id (optional; defaults to the 'default' org), event_type,
// target_type, target_id, limit, offset.
func (h *AuditHandler) ListAuditEvents(w http.ResponseWriter, r *http.Request) {
	var orgID uuid.UUID
	if orgIDStr := r.URL.Query().Get("org_id"); orgIDStr != "" {
		id, ok := parseUUID(w, orgIDStr)
		if !ok {
			return
		}
		orgID = id
	} else {
		id, err := h.repo.DefaultOrgID(r.Context())
		if err != nil {
			slog.Error("resolve default org for audit", "err", err)
			writeError(w, http.StatusInternalServerError, "db_error", "Failed to resolve organisation")
			return
		}
		orgID = id
	}

	var eventType *audit.EventType
	if raw := r.URL.Query().Get("event_type"); raw != "" {
		et := audit.EventType(raw)
		eventType = &et
	}

	targetType := r.URL.Query().Get("target_type")
	targetID := r.URL.Query().Get("target_id")
	if targetID != "" {
		if _, ok := parseUUID(w, targetID); !ok {
			return
		}
	}

	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var l int
		if _, err := fmt.Sscanf(raw, "%d", &l); err == nil && l > 0 && l <= 200 {
			limit = l
		}
	}

	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		var o int
		if _, err := fmt.Sscanf(raw, "%d", &o); err == nil && o >= 0 {
			offset = o
		}
	}

	events, err := h.repo.List(r.Context(), orgID, eventType, targetType, targetID, limit, offset)
	if err != nil {
		slog.Error("list audit events", "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to list audit events")
		return
	}
	if events == nil {
		events = []audit.Event{}
	}
	writeJSON(w, http.StatusOK, events)
}
