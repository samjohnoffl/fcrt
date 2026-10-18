// Package api — handler_experiment.go
//
// HTTP handlers for /api/v1/experiments.
//
// Key invariants enforced here:
//   - AC-03.2: experiments referencing a scenario_proposal require status=approved.
//   - AC-04.1: baseline + variant run created per experiment.
//   - AC-06.2: evidence bundle endpoint returns all required keys.
//   - T-05: checksum returned with every evidence response; frontend verifies it.
package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/coverage"
	"github.com/yourorg/financial-crime-red-team/internal/evidence"
	"github.com/yourorg/financial-crime-red-team/internal/experiment"
	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

// ExperimentHandler handles experiment endpoints.
type ExperimentHandler struct {
	expRepo      *experiment.Repository
	bankRepo     *bank.Repository
	scenarioRepo *scenario.Repository
	covRepo      *coverage.Repository
	evidenceRepo *evidence.Repository
}

// NewExperimentHandler creates an ExperimentHandler.
func NewExperimentHandler(
	expRepo *experiment.Repository,
	bankRepo *bank.Repository,
	scenarioRepo *scenario.Repository,
	covRepo *coverage.Repository,
	evidenceRepo *evidence.Repository,
) *ExperimentHandler {
	return &ExperimentHandler{
		expRepo:      expRepo,
		bankRepo:     bankRepo,
		scenarioRepo: scenarioRepo,
		covRepo:      covRepo,
		evidenceRepo: evidenceRepo,
	}
}

// ─── GET /api/v1/experiments ─────────────────────────────────────────────────

// ListExperiments is intentionally minimal: the experiment_pair table is queried.
// Full pagination is a post-v1 enhancement (DECISIONS.md).
// experimentListItem flattens a Pair with its outcome summary for list views.
type experimentListItem struct {
	experiment.Pair
	EvasionDelta        *int     `json:"evasion_delta,omitempty"`
	NewAlertRuleIDs     []string `json:"new_alert_rule_ids,omitempty"`
	DroppedAlertRuleIDs []string `json:"dropped_alert_rule_ids,omitempty"`
	OutcomeUnchanged    *bool    `json:"outcome_unchanged,omitempty"`
}

// ListExperiments returns all experiment pairs, newest first, with their
// outcome summary (evasion delta + rule diff) when available.
func (h *ExperimentHandler) ListExperiments(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	summaries, err := h.expRepo.ListPairSummaries(r.Context(), limit)
	if err != nil {
		slog.Error("list experiments", "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to list experiments")
		return
	}

	items := make([]experimentListItem, 0, len(summaries))
	for _, s := range summaries {
		item := experimentListItem{Pair: s.Pair}
		if s.Outcome != nil {
			d := s.Outcome.EvasionDelta
			u := s.Outcome.OutcomeUnchanged
			item.EvasionDelta = &d
			item.OutcomeUnchanged = &u
			item.NewAlertRuleIDs = ruleIDsToStr(s.Outcome.NewAlertRuleIDs)
			item.DroppedAlertRuleIDs = ruleIDsToStr(s.Outcome.DroppedAlertRuleIDs)
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, items)
}

// ─── POST /api/v1/experiments ────────────────────────────────────────────────

type createExperimentRequest struct {
	CohortID           uuid.UUID      `json:"cohort_id"`
	ScenarioProposalID *uuid.UUID     `json:"scenario_proposal_id"` // optional
	Name               string         `json:"name"`
	DeclaredChange     string         `json:"declared_change"`
	MutationType       string         `json:"mutation_type"`
	MutationSpec       map[string]any `json:"mutation_spec"`
}

// CreateExperiment validates inputs and runs the experiment pair synchronously.
// AC-03.2: if scenario_proposal_id is provided, it must be approved.
func (h *ExperimentHandler) CreateExperiment(w http.ResponseWriter, r *http.Request) {
	var req createExperimentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Request body must be valid JSON")
		return
	}
	if req.CohortID == uuid.Nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "cohort_id is required")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "name is required")
		return
	}
	if req.DeclaredChange == "" {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "declared_change is required")
		return
	}

	// AC-03.2: reject experiments that reference a non-approved scenario.
	if req.ScenarioProposalID != nil {
		approved, err := h.scenarioRepo.IsApproved(r.Context(), *req.ScenarioProposalID)
		if err != nil {
			if errors.Is(err, scenario.ErrProposalNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "Scenario proposal not found")
				return
			}
			slog.Error("check scenario approval", "err", err)
			writeError(w, http.StatusInternalServerError, "db_error", "Failed to check scenario status")
			return
		}
		if !approved {
			// AC-03.2: HTTP 422 with code "scenario_not_approved"
			writeError(w, http.StatusUnprocessableEntity, "scenario_not_approved",
				"Referenced scenario proposal must be approved before creating an experiment")
			return
		}
	}

	// Cohort must be locked (AC-01.2 enforced at bank layer, but validate here for better UX).
	cohort, err := h.bankRepo.GetCohort(r.Context(), req.CohortID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "not_found", "Cohort not found")
			return
		}
		slog.Error("get cohort for experiment", "cohort_id", req.CohortID, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get cohort")
		return
	}
	if cohort.LockedAt == nil {
		writeError(w, http.StatusConflict, "cohort_not_locked",
			"Cohort must be locked before running an experiment")
		return
	}

	// Build the batch selection for the cohort's full window.
	windowStart := time.Now().AddDate(0, 0, -90)
	windowEnd := time.Now()
	if cohort.WindowStart != nil {
		windowStart = *cohort.WindowStart
	}
	if cohort.WindowEnd != nil {
		windowEnd = *cohort.WindowEnd
	}

	sel, err := h.bankRepo.SelectBatch(r.Context(), req.CohortID, windowStart, windowEnd)
	if err != nil {
		slog.Error("select batch", "cohort_id", req.CohortID, "err", err)
		writeError(w, http.StatusInternalServerError, "batch_error", "Failed to load cohort data")
		return
	}

	// Build PairSpec and run the experiment (AC-04.1, AC-04.3).
	cfg := aml.DefaultControlConfig() // *aml.ControlConfig
	inputCtx, err := experiment.NewInputContext(sel.Manifest, windowStart, windowEnd)
	if err != nil {
		slog.Error("build input context", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to build input context")
		return
	}

	mt := experiment.MutationType(req.MutationType)
	if mt == "" {
		mt = experiment.MutationSplitTransaction
	}

	spec := experiment.PairSpec{
		Name:           req.Name,
		Input:          inputCtx,
		Config:         cfg,
		DeclaredChange: req.DeclaredChange,
		MutationType:   mt,
		MutationSpec:   req.MutationSpec,
	}

	result, err := experiment.Execute(spec, sel, time.Now())
	if err != nil {
		if errors.Is(err, experiment.ErrInvalidPairSpec) {
			writeError(w, http.StatusUnprocessableEntity, "invalid_spec", err.Error())
			return
		}
		slog.Error("execute experiment", "err", err)
		writeError(w, http.StatusInternalServerError, "experiment_error", "Experiment execution failed")
		return
	}

	// Persist the result (AC-04.1, AC-04.2, AC-04.3).
	if err := h.expRepo.SavePairResult(r.Context(), result); err != nil {
		slog.Error("save experiment result", "pair_id", result.Pair.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to persist experiment result")
		return
	}

	// Update coverage (AC-05.1).
	allRules := append(result.Outcome.NewAlertRuleIDs, result.Outcome.DroppedAlertRuleIDs...)
	for _, r2 := range result.Baseline.RawAlerts {
		allRules = append(allRules, r2.RuleID)
	}
	if len(allRules) > 0 {
		if err := h.covRepo.RecordTriggers(r.Context(), allRules, cfg.ControlVersion); err != nil {
			slog.Warn("record coverage triggers", "err", err) // non-fatal
		}
	}

	// Persist the evidence bundle (PRODUCT_SPEC item 7). Non-fatal: the
	// experiment is already committed and reproducible via the outcome.
	if h.evidenceRepo != nil {
		if bundle, err := h.buildEvidenceBundle(r.Context(), result.Pair); err != nil {
			slog.Warn("build evidence bundle", "pair_id", result.Pair.ID, "err", err)
		} else if err := h.evidenceRepo.Save(r.Context(), bundle); err != nil {
			slog.Warn("persist evidence bundle", "pair_id", result.Pair.ID, "err", err)
		}
	}

	writeJSON(w, http.StatusCreated, result.Pair)
}

// ─── GET /api/v1/experiments/{id} ────────────────────────────────────────────

// GetExperiment returns the pair record plus outcome diff.
func (h *ExperimentHandler) GetExperiment(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	pair, err := h.expRepo.GetPair(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "not_found", "Experiment not found")
			return
		}
		slog.Error("get experiment", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get experiment")
		return
	}
	outcome, err := h.expRepo.GetOutcome(r.Context(), id)
	if err != nil && !isNotFound(err) {
		slog.Warn("get outcome", "pair_id", id, "err", err)
	}
	runs, err := h.expRepo.ListRunsForPair(r.Context(), id)
	if err != nil {
		slog.Warn("list runs", "pair_id", id, "err", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"pair":    pair,
		"outcome": outcome,
		"runs":    runs,
	})
}

// ─── GET /api/v1/experiments/{id}/evidence ───────────────────────────────────

// evidenceResponse is the API response for AC-06.2: the persisted bundle plus
// honest limitation labels. All required keys are always present.
type evidenceResponse struct {
	evidence.Bundle
	Limitations []string `json:"limitations"`
}

// evidenceLimitations documents what an evidence bundle cannot prove (AC-06).
func evidenceLimitations(controlHash string) []string {
	return []string{
		"This evidence bundle covers synthetic data only — no real transactions.",
		"AML rule version is pinned to " + controlHash + "; re-running with a different version may produce different results.",
		"Gemini scenario generation is probabilistic; identical cohorts may produce different proposals across runs.",
		"Coverage weight reflects historical trigger counts only — rules not present in the DB have unknown coverage.",
		"Coverage delta is not snapshotted per run: coverage_delta is empty by design to keep the bundle reproducible (see GET /api/v1/coverage for cumulative state).",
	}
}

// buildEvidenceBundle assembles the reproducible evidence artifact for a pair
// from the persisted runs and outcome. CreatedAt is derived from stable data so
// the checksum is reproducible across calls.
func (h *ExperimentHandler) buildEvidenceBundle(ctx context.Context, pair experiment.Pair) (evidence.Bundle, error) {
	outcome, err := h.expRepo.GetOutcome(ctx, pair.ID)
	if err != nil {
		return evidence.Bundle{}, fmt.Errorf("get outcome: %w", err)
	}
	runs, err := h.expRepo.ListRunsForPair(ctx, pair.ID)
	if err != nil {
		return evidence.Bundle{}, fmt.Errorf("list runs: %w", err)
	}

	var baselineAlerts, variantAlerts []aml.Alert
	var baselineCount, variantCount int
	for _, rr := range runs {
		switch rr.RunType {
		case experiment.RunTypeBaseline:
			baselineAlerts = rr.RawAlerts
			baselineCount = rr.AlertCount
		case experiment.RunTypeVariant:
			variantAlerts = rr.RawAlerts
			variantCount = rr.AlertCount
		}
	}

	snapshot := evidence.CohortSnapshot{CohortID: pair.CohortID, Seed: pair.Seed}
	if cohort, err := h.bankRepo.GetCohort(ctx, pair.CohortID); err == nil {
		snapshot.Predicate = cohort.Predicate
		snapshot.EntityCount = cohort.Manifest.EntityCount
		snapshot.TxCount = cohort.Manifest.TxCount
		if cohort.LockedAt != nil {
			snapshot.LockedAt = *cohort.LockedAt
		}
	}

	createdAt := outcome.CreatedAt
	if createdAt.IsZero() {
		createdAt = pair.UpdatedAt
	}

	if baselineAlerts == nil {
		baselineAlerts = []aml.Alert{}
	}
	if variantAlerts == nil {
		variantAlerts = []aml.Alert{}
	}

	b := evidence.Bundle{
		RunID:          pair.ID,
		AMLRuleVersion: pair.ControlHash,
		CohortSnapshot: snapshot,
		BaselineAlerts: baselineAlerts,
		VariantAlerts:  variantAlerts,
		AMLDiff: aml.AlertDiff{
			BaselineAlertCount:  baselineCount,
			VariantAlertCount:   variantCount,
			EvasionDelta:        outcome.EvasionDelta,
			NewAlertRuleIDs:     outcome.NewAlertRuleIDs,
			DroppedAlertRuleIDs: outcome.DroppedAlertRuleIDs,
		},
		CoverageDelta: []coverage.RuleCoverage{},
		CreatedAt:     createdAt,
	}
	sum, err := evidence.ComputeChecksum(b)
	if err != nil {
		return evidence.Bundle{}, err
	}
	b.Checksum = sum
	return b, nil
}

// GetEvidence returns the evidence bundle (AC-06.2, T-05).
// It serves the persisted bundle when available, otherwise builds and persists
// one. Pass ?download=1 to receive it as a JSON file attachment.
func (h *ExperimentHandler) GetEvidence(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	pair, err := h.expRepo.GetPair(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "not_found", "Experiment not found")
			return
		}
		slog.Error("get pair for evidence", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get experiment")
		return
	}

	// Prefer the persisted bundle (immutable artifact); fall back to building it.
	var bundle evidence.Bundle
	if h.evidenceRepo != nil {
		if got, err := h.evidenceRepo.Get(r.Context(), id); err == nil {
			bundle = got
		} else if !errors.Is(err, evidence.ErrBundleNotFound) {
			slog.Warn("get persisted evidence bundle", "pair_id", id, "err", err)
		}
	}
	if bundle.RunID == uuid.Nil {
		built, err := h.buildEvidenceBundle(r.Context(), pair)
		if err != nil {
			slog.Error("build evidence bundle", "pair_id", id, "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to build evidence bundle")
			return
		}
		bundle = built
		if h.evidenceRepo != nil {
			if err := h.evidenceRepo.Save(r.Context(), built); err != nil {
				slog.Warn("persist evidence bundle", "pair_id", id, "err", err) // non-fatal
			}
		}
	}

	resp := evidenceResponse{Bundle: bundle, Limitations: evidenceLimitations(pair.ControlHash)}

	if r.URL.Query().Get("download") != "" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=\"evidence_%s.json\"", id))
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(resp); err != nil {
			slog.Error("write evidence download", "pair_id", id, "err", err)
		}
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// ─── POST /api/v1/experiments/{id}/replay ────────────────────────────────────

// replayResponse is the result of a regression replay (AC-06.1, T-05).
type replayResponse struct {
	OriginalChecksum  string `json:"original_checksum"`
	ReplayedChecksum  string `json:"replayed_checksum"`
	ChecksumMatch     bool   `json:"checksum_match"`
	EvasionDeltaMatch bool   `json:"evasion_delta_match"`
	OriginalDelta     int    `json:"original_delta"`
	ReplayedDelta     int    `json:"replayed_delta"`
	// Limitations are always present and honest.
	Limitations []string `json:"limitations"`
}

// ReplayExperiment re-runs the AML evaluator on the stored input context
// and verifies the result matches the stored outcome (AC-06.1, T-05).
func (h *ExperimentHandler) ReplayExperiment(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	pair, err := h.expRepo.GetPair(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "not_found", "Experiment not found")
			return
		}
		slog.Error("get pair for replay", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get experiment")
		return
	}

	originalOutcome, err := h.expRepo.GetOutcome(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "not_found", "No outcome found for experiment — it may not have completed")
			return
		}
		slog.Error("get outcome for replay", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to get outcome")
		return
	}

	// Reconstruct a minimal PairSpec from the stored pair and re-execute.
	cohort, err := h.bankRepo.GetCohort(r.Context(), pair.CohortID)
	if err != nil {
		slog.Error("get cohort for replay", "cohort_id", pair.CohortID, "err", err)
		writeError(w, http.StatusInternalServerError, "db_error", "Failed to load cohort")
		return
	}

	windowStart := time.Now().AddDate(0, 0, -90)
	windowEnd := time.Now()
	if cohort.WindowStart != nil {
		windowStart = *cohort.WindowStart
	}
	if cohort.WindowEnd != nil {
		windowEnd = *cohort.WindowEnd
	}

	sel, err := h.bankRepo.SelectBatch(r.Context(), pair.CohortID, windowStart, windowEnd)
	if err != nil {
		slog.Error("select batch for replay", "cohort_id", pair.CohortID, "err", err)
		writeError(w, http.StatusInternalServerError, "batch_error", "Failed to load cohort data for replay")
		return
	}

	cfg := aml.DefaultControlConfig() // *aml.ControlConfig
	inputCtx, err := experiment.NewInputContext(sel.Manifest, windowStart, windowEnd)
	if err != nil {
		slog.Error("build input context for replay", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to build input context")
		return
	}

	spec := experiment.PairSpec{
		Name:           pair.Name + " [replay]",
		Input:          inputCtx,
		Config:         cfg,
		DeclaredChange: pair.DeclaredChange,
		MutationType:   pair.MutationType,
		MutationSpec:   pair.MutationSpec,
	}

	replayResult, err := experiment.Execute(spec, sel, time.Now())
	if err != nil {
		slog.Error("replay experiment", "pair_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "replay_error", "Replay execution failed")
		return
	}

	// Build the original evidence bundle checksum from the stored pair.
	origBundle := map[string]any{
		"cohort_id":              pair.CohortID,
		"manifest_hash":          pair.ManifestHash,
		"control_hash":           pair.ControlHash,
		"evasion_delta":          originalOutcome.EvasionDelta,
		"new_alert_rule_ids":     originalOutcome.NewAlertRuleIDs,
		"dropped_alert_rule_ids": originalOutcome.DroppedAlertRuleIDs,
	}
	origBytes, _ := json.Marshal(origBundle)
	origSum := sha256.Sum256(origBytes)

	// Build the replayed bundle checksum.
	replayBundle := map[string]any{
		"cohort_id":              pair.CohortID,
		"manifest_hash":          replayResult.Pair.ManifestHash,
		"control_hash":           replayResult.Pair.ControlHash,
		"evasion_delta":          replayResult.Outcome.EvasionDelta,
		"new_alert_rule_ids":     replayResult.Outcome.NewAlertRuleIDs,
		"dropped_alert_rule_ids": replayResult.Outcome.DroppedAlertRuleIDs,
	}
	replayBytes, _ := json.Marshal(replayBundle)
	replaySum := sha256.Sum256(replayBytes)

	origHex := fmt.Sprintf("%x", origSum)
	replayHex := fmt.Sprintf("%x", replaySum)

	resp := replayResponse{
		OriginalChecksum:  origHex,
		ReplayedChecksum:  replayHex,
		ChecksumMatch:     origHex == replayHex,
		EvasionDeltaMatch: originalOutcome.EvasionDelta == replayResult.Outcome.EvasionDelta,
		OriginalDelta:     originalOutcome.EvasionDelta,
		ReplayedDelta:     replayResult.Outcome.EvasionDelta,
		Limitations: []string{
			"Replay uses the current cohort data — if cohort was re-generated, results may differ.",
			"Gemini scenario generation is NOT replayed; only the AML evaluator is re-run.",
			"Checksum covers AML diff fields only, not the full evidence bundle.",
		},
	}

	writeJSON(w, http.StatusOK, resp)
}
