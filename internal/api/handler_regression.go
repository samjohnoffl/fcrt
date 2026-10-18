// Package api — handler_regression.go
//
// HTTP handlers for the Regression Test Pack (PRODUCT_SPEC item 7).
//
// Endpoints:
//
//	GET  /api/v1/regression/pack          build & download a pack from completed experiments
//	POST /api/v1/regression/verify        verify a previously exported pack against the DB
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/experiment"
	"github.com/yourorg/financial-crime-red-team/internal/regression"
)

// RegressionHandler builds and verifies regression test packs.
type RegressionHandler struct {
	expRepo  *experiment.Repository
	bankRepo *bank.Repository
}

// NewRegressionHandler creates a RegressionHandler.
func NewRegressionHandler(expRepo *experiment.Repository, bankRepo *bank.Repository) *RegressionHandler {
	return &RegressionHandler{expRepo: expRepo, bankRepo: bankRepo}
}

// GetPack builds a Regression Test Pack from all completed experiment pairs and
// streams it as a JSON download.
//
// Query params:
//
//	limit — max number of cases (default 1000)
//
// GET /api/v1/regression/pack
func (h *RegressionHandler) GetPack(w http.ResponseWriter, r *http.Request) {
	limit := 1000
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := parseInt(v); err == nil && n > 0 {
			limit = n
		}
	}

	pairs, err := h.expRepo.ListCompletedPairsWithOutcomes(r.Context(), limit)
	if err != nil {
		slog.Error("list completed pairs for regression pack", "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to list completed experiments")
		return
	}

	cases := make([]regression.Case, 0, len(pairs))
	for _, cp := range pairs {
		// The regression case needs the frozen window; it is stored on the cohort.
		cohort, err := h.bankRepo.GetCohort(r.Context(), cp.Pair.CohortID)
		if err != nil {
			slog.Warn("skip pair: cohort unavailable", "pair_id", cp.Pair.ID, "err", err)
			continue
		}
		winStart, winEnd := windowOf(cohort)

		cases = append(cases, regression.Case{
			ExperimentID:   cp.Pair.ID,
			Name:           cp.Pair.Name,
			CohortID:       cp.Pair.CohortID,
			ManifestHash:   cp.Pair.ManifestHash,
			ControlHash:    cp.Pair.ControlHash,
			WindowStart:    winStart,
			WindowEnd:      winEnd,
			DeclaredChange: cp.Pair.DeclaredChange,
			MutationType:   string(cp.Pair.MutationType),
			MutationSpec:   cp.Pair.MutationSpec,
			Expected: regression.ExpectedDiff{
				EvasionDelta:        cp.Outcome.EvasionDelta,
				NewAlertRuleIDs:     ruleIDsToStr(cp.Outcome.NewAlertRuleIDs),
				DroppedAlertRuleIDs: ruleIDsToStr(cp.Outcome.DroppedAlertRuleIDs),
				OutcomeUnchanged:    cp.Outcome.OutcomeUnchanged,
			},
		})
	}

	pack, err := regression.Build(cases, aml.ControlVersion, time.Now())
	if err != nil {
		slog.Error("build regression pack", "err", err)
		writeError(w, http.StatusInternalServerError, "build_error", "Failed to build regression pack")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="regression-pack.json"`)
	w.WriteHeader(http.StatusOK)
	if err := pack.Export(w); err != nil {
		slog.Error("export regression pack", "err", err)
	}
}

// verifyRequest is the body for POST /api/v1/regression/verify — a previously
// exported pack to be re-checked against a freshly-computed diff.
type verifyResponse struct {
	OK          bool                    `json:"ok"`
	PackVersion string                  `json:"pack_version"`
	Total       int                     `json:"total"`
	Passed      int                     `json:"passed"`
	Failed      int                     `json:"failed"`
	Results     []regression.CaseResult `json:"results"`
}

// VerifyPack re-imports an exported pack, validates its checksum, and reports
// the pack-level integrity result. Full re-execution requires cohort data and is
// performed by the worker; this endpoint validates structure and checksum only.
//
// POST /api/v1/regression/verify
func (h *RegressionHandler) VerifyPack(w http.ResponseWriter, r *http.Request) {
	pack, err := regression.Import(r.Body)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_pack", err.Error())
		return
	}

	ok, err := pack.VerifyChecksum()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checksum_error", err.Error())
		return
	}

	resp := verifyResponse{
		OK:          ok,
		PackVersion: pack.Version,
		Total:       len(pack.Cases),
		Passed:      len(pack.Cases),
		Failed:      0,
		Results:     []regression.CaseResult{},
	}
	if !ok {
		resp.OK = false
		resp.Passed = 0
		resp.Failed = len(pack.Cases)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func windowOf(c bank.Cohort) (time.Time, time.Time) {
	start := time.Now().AddDate(0, 0, -90)
	end := time.Now()
	if c.WindowStart != nil {
		start = *c.WindowStart
	}
	if c.WindowEnd != nil {
		end = *c.WindowEnd
	}
	return start, end
}

func ruleIDsToStr(ids []aml.RuleID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}

func parseInt(s string) (int, error) {
	var n int
	err := json.Unmarshal([]byte(s), &n)
	return n, err
}
