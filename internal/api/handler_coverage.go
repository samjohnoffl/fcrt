// Package api — handler_coverage.go
//
// HTTP handler for /api/v1/coverage.
// Returns live coverage summary from the rule_coverage table (AC-05.1, AC-05.2).
package api

import (
	"log/slog"
	"net/http"

	"github.com/yourorg/financial-crime-red-team/internal/coverage"
)

// CoverageHandler handles coverage endpoints.
type CoverageHandler struct {
	repo *coverage.Repository
}

// NewCoverageHandler creates a CoverageHandler.
func NewCoverageHandler(repo *coverage.Repository) *CoverageHandler {
	return &CoverageHandler{repo: repo}
}

// GetCoverage returns the current rule coverage summary (AC-05.1, AC-05.2).
// Response always includes total_rules, covered_rules, coverage_pct, and
// per-rule details sorted by coverage_weight DESC (uncovered rules first).
func (h *CoverageHandler) GetCoverage(w http.ResponseWriter, r *http.Request) {
	summary, err := h.repo.GetSummary(r.Context())
	if err != nil {
		slog.Error("get coverage summary", "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get coverage summary")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
