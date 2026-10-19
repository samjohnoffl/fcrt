// Package api — handler_counterfactual.go
//
// POST /api/v1/counterfactuals/validate
//
// Validates a baseline/variant genome pair and returns the invariance report:
// declared changes, unchanged-dimension deviations, dependent changes and the
// tightly/partially/exploratory classification.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/yourorg/financial-crime-red-team/internal/counterfactual"
	"github.com/yourorg/financial-crime-red-team/internal/genome"
)

// CounterfactualHandler validates genome pairs.
type CounterfactualHandler struct{}

// NewCounterfactualHandler creates a CounterfactualHandler.
func NewCounterfactualHandler() *CounterfactualHandler {
	return &CounterfactualHandler{}
}

type counterfactualRequest struct {
	Baseline           genome.Genome `json:"baseline"`
	Variant            genome.Genome `json:"variant"`
	DeclaredDimensions []string      `json:"declared_dimensions"`
}

// Validate compares a baseline and variant genome.
//
// A structurally invalid pair is returned with HTTP 200 and valid=false: the
// request was processed correctly, the answer is that the pair is rejected.
// Malformed JSON or a genome that fails schema validation returns 422.
func (h *CounterfactualHandler) Validate(w http.ResponseWriter, r *http.Request) {
	var req counterfactualRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Request body is not valid JSON")
		return
	}

	if err := req.Baseline.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_genome", "Baseline genome: "+err.Error())
		return
	}
	if err := req.Variant.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_genome", "Variant genome: "+err.Error())
		return
	}
	if len(req.DeclaredDimensions) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "no_declared_change",
			"declared_dimensions must name at least one genome dimension")
		return
	}

	report := counterfactual.Compare(req.Baseline, req.Variant, req.DeclaredDimensions)
	slog.Info("counterfactual validated",
		"valid", report.Valid,
		"classification", report.Classification,
		"declared", report.DeclaredDimensions,
		"violations", len(report.Violations))
	writeJSON(w, http.StatusOK, report)
}
