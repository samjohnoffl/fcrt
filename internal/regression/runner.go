package regression

import (
	"context"
	"fmt"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/experiment"
)

// BatchProvider loads the frozen cohort batch required to re-execute a case.
// In production this is backed by bank.Repository.SelectBatch; in tests it can
// return a synthetic batch so the suite runs without a database.
type BatchProvider func(ctx context.Context, c Case) (*bank.BatchSelection, error)

// PackResult aggregates the results of running every case in a pack.
type PackResult struct {
	Version      string       `json:"version"`
	AMLVersion   string       `json:"aml_rule_version"`
	Total        int          `json:"total"`
	Passed       int          `json:"passed"`
	Failed       int          `json:"failed"`
	Results      []CaseResult `json:"results"`
	PackChecksum string       `json:"pack_checksum"`
}

// OK reports whether every case passed.
func (r PackResult) OK() bool { return r.Failed == 0 }

// RunCase re-executes one case deterministically and asserts the AML diff.
//
// Determinism is achieved because experiment.Execute is a pure function of
// (PairSpec, BatchSelection): the cohort is re-selected from its frozen seed and
// window, and the mutation is applied in-memory with deterministic tx IDs.
func RunCase(ctx context.Context, c Case, provider BatchProvider) (CaseResult, error) {
	sel, err := provider(ctx, c)
	if err != nil {
		return CaseResult{}, fmt.Errorf("regression: load batch for %q: %w", c.Name, err)
	}

	inputCtx, err := experiment.NewInputContext(sel.Manifest, c.WindowStart, c.WindowEnd)
	if err != nil {
		return CaseResult{}, fmt.Errorf("regression: build input context for %q: %w", c.Name, err)
	}

	cfg := aml.DefaultControlConfig()

	spec := experiment.PairSpec{
		Name:           c.Name,
		Input:          inputCtx,
		Config:         cfg,
		DeclaredChange: c.DeclaredChange,
		MutationType:   experiment.MutationType(c.MutationType),
		MutationSpec:   c.MutationSpec,
	}

	res, err := experiment.Execute(spec, sel, c.WindowStart)
	if err != nil {
		return CaseResult{}, fmt.Errorf("regression: execute case %q: %w", c.Name, err)
	}

	actual := ExpectedDiff{
		EvasionDelta:        res.Outcome.EvasionDelta,
		NewAlertRuleIDs:     ruleIDsToStrings(res.Outcome.NewAlertRuleIDs),
		DroppedAlertRuleIDs: ruleIDsToStrings(res.Outcome.DroppedAlertRuleIDs),
		OutcomeUnchanged:    res.Outcome.OutcomeUnchanged,
	}

	cr := CompareCase(c, actual)

	// Record control-hash drift as a mismatch: if the pinned AML config changed,
	// the regression run is no longer comparable to the recorded expectation.
	if c.ControlHash != "" && c.ControlHash != cfg.ConfigHash {
		cr.Passed = false
		cr.Mismatches = append(cr.Mismatches,
			fmt.Sprintf("control_hash: expected %s, got %s (AML config drift)", c.ControlHash, cfg.ConfigHash))
	}

	return cr, nil
}

// RunPack executes every case in the pack and aggregates the results.
func RunPack(ctx context.Context, p Pack, provider BatchProvider) (PackResult, error) {
	out := PackResult{
		Version:      p.Version,
		AMLVersion:   p.AMLVersion,
		Total:        len(p.Cases),
		PackChecksum: p.Checksum,
	}
	for _, c := range p.Cases {
		cr, err := RunCase(ctx, c, provider)
		if err != nil {
			return PackResult{}, err
		}
		if cr.Passed {
			out.Passed++
		} else {
			out.Failed++
		}
		out.Results = append(out.Results, cr)
	}
	return out, nil
}

// ruleIDsToStrings converts aml.RuleID slices to plain strings for JSON.
func ruleIDsToStrings(ids []aml.RuleID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}
