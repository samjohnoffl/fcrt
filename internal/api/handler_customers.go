// Package api — handler_customers.go
//
// GET /api/v1/customers
//
// Lists synthetic customers (entities) so the UI can show the population that
// scenarios and controls operate on. Optional ?cohort_id= narrows the list.
package api

import (
	"log/slog"
	"net/http"

	"github.com/yourorg/financial-crime-red-team/internal/bank"
)

// CustomerHandler serves the synthetic customer directory.
type CustomerHandler struct {
	bankRepo *bank.Repository
}

// NewCustomerHandler creates a CustomerHandler.
func NewCustomerHandler(bankRepo *bank.Repository) *CustomerHandler {
	return &CustomerHandler{bankRepo: bankRepo}
}

// List returns entities, newest first. A cohort_id query parameter restricts
// the result to that cohort; without it, all cohorts are included.
func (h *CustomerHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if raw := r.URL.Query().Get("cohort_id"); raw != "" {
		cohortID, ok := parseUUID(w, raw)
		if !ok {
			return
		}
		entities, err := h.bankRepo.ListEntities(ctx, cohortID)
		if err != nil {
			slog.Error("list customers", "cohort_id", cohortID, "err", err)
			writeError(w, http.StatusInternalServerError, "db_error", "Failed to list customers")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"customers": entities, "count": len(entities)})
		return
	}

	entities, err := h.bankRepo.ListAllEntities(ctx)
	if err != nil {
		slog.Error("list all customers", "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to list customers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"customers": entities, "count": len(entities)})
}
