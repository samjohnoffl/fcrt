// Package experiment — pair.go
//
// Executor runs an experiment pair: evaluates the AML harness on the baseline
// (unmodified) and variant (mutated) BatchSelection, then computes the Outcome.
//
// Key guarantees:
//   - Both runs use exactly the same ControlConfig (verified via ConfigHash).
//   - Both runs use the same frozen input (verified via ManifestHash).
//   - Incompatible pairs (mismatched hashes) are rejected before evaluation.
//   - All alert IDs and ordering are deterministic (delegated to Evaluator).
//   - Mutations are applied in-memory; the DB cohort data is never modified.
package experiment

import (
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
	"github.com/yourorg/financial-crime-red-team/internal/bank"
)

// ExecuteResult is the full output of a pair execution.
type ExecuteResult struct {
	Pair     Pair
	Baseline RunResult
	Variant  RunResult
	Outcome  Outcome
}

// Execute validates the PairSpec, applies the mutation to a copy of the
// BatchSelection, evaluates both runs deterministically, and computes the Outcome.
//
// It does NOT persist anything to the database; that is the caller's responsibility
// via Repository.SavePairResult.
//
// Clock: startedAt is passed in (not read from wall clock) so tests can control it.
func Execute(spec PairSpec, sel *bank.BatchSelection, startedAt time.Time) (*ExecuteResult, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	// Verify the selection's manifest matches the spec's InputContext.
	selManifestHash, err := hashManifest(sel.Manifest)
	if err != nil {
		return nil, fmt.Errorf("experiment: hash selection manifest: %w", err)
	}
	if selManifestHash != spec.Input.ManifestHash {
		return nil, fmt.Errorf("%w: selection manifest_hash=%s, spec manifest_hash=%s",
			ErrIncomparablePair, selManifestHash, spec.Input.ManifestHash)
	}

	ev, err := aml.NewEvaluator(spec.Config)
	if err != nil {
		return nil, fmt.Errorf("experiment: create evaluator: %w", err)
	}

	pairID := uuid.New()
	configHash := spec.Config.ConfigHash
	manifestHash := spec.Input.ManifestHash

	// ── Baseline run ─────────────────────────────────────────────────────────
	baselineResult := ev.Evaluate(sel)
	baselineID := uuid.New()
	baselineCompleted := startedAt // instant for deterministic tests

	baseline := RunResult{
		ID:           baselineID,
		PairID:       pairID,
		RunType:      RunTypeBaseline,
		ControlHash:  configHash,
		ManifestHash: manifestHash,
		AlertCount:   len(baselineResult.Alerts),
		AlertSummary: summariseAlerts(baselineResult.Alerts),
		RawAlerts:    baselineResult.Alerts,
		StartedAt:    startedAt,
		CompletedAt:  baselineCompleted,
	}

	// ── Variant run ───────────────────────────────────────────────────────────
	mutated, err := applyMutation(sel, spec.MutationType, spec.MutationSpec)
	if err != nil {
		return nil, fmt.Errorf("experiment: apply mutation %s: %w", spec.MutationType, err)
	}
	variantResult := ev.Evaluate(mutated)
	variantID := uuid.New()

	variant := RunResult{
		ID:           variantID,
		PairID:       pairID,
		RunType:      RunTypeVariant,
		ControlHash:  configHash,
		ManifestHash: manifestHash,
		AlertCount:   len(variantResult.Alerts),
		AlertSummary: summariseAlerts(variantResult.Alerts),
		RawAlerts:    variantResult.Alerts,
		StartedAt:    startedAt,
		CompletedAt:  startedAt,
	}

	// ── Outcome comparison ────────────────────────────────────────────────────
	outcome := compareRuns(pairID, baselineID, variantID, baseline.RawAlerts, variant.RawAlerts)

	// ── Pair record ───────────────────────────────────────────────────────────
	pair := Pair{
		ID:             pairID,
		Name:           spec.Name,
		CohortID:       spec.Input.CohortID,
		ControlHash:    configHash,
		ManifestHash:   manifestHash,
		Seed:           spec.Input.Manifest.Seed,
		DeclaredChange: spec.DeclaredChange,
		MutationType:   spec.MutationType,
		MutationSpec:   spec.MutationSpec,
		Status:         StatusCompleted,
		CreatedAt:      startedAt,
		UpdatedAt:      startedAt,
	}

	return &ExecuteResult{
		Pair:     pair,
		Baseline: baseline,
		Variant:  variant,
		Outcome:  outcome,
	}, nil
}

// ─── Outcome comparison ───────────────────────────────────────────────────────

// compareRuns computes the Outcome from two alert sets.
// It is a pure function: no DB access, no I/O.
func compareRuns(
	pairID, baselineRunID, variantRunID uuid.UUID,
	baselineAlerts, variantAlerts []aml.Alert,
) Outcome {
	baselineRules := ruleIDSet(baselineAlerts)
	variantRules := ruleIDSet(variantAlerts)

	var newRules, droppedRules []aml.RuleID

	for r := range variantRules {
		if !baselineRules[r] {
			newRules = append(newRules, r)
		}
	}
	for r := range baselineRules {
		if !variantRules[r] {
			droppedRules = append(droppedRules, r)
		}
	}

	// Sort for deterministic output.
	sort.Slice(newRules, func(i, j int) bool { return string(newRules[i]) < string(newRules[j]) })
	sort.Slice(droppedRules, func(i, j int) bool { return string(droppedRules[i]) < string(droppedRules[j]) })

	evasionDelta := len(baselineAlerts) - len(variantAlerts)
	unchanged := evasionDelta == 0 && len(newRules) == 0 && len(droppedRules) == 0

	return Outcome{
		ID:                  uuid.New(),
		PairID:              pairID,
		BaselineRunID:       baselineRunID,
		VariantRunID:        variantRunID,
		EvasionDelta:        evasionDelta,
		NewAlertRuleIDs:     newRules,
		DroppedAlertRuleIDs: droppedRules,
		OutcomeUnchanged:    unchanged,
		CreatedAt:           time.Now(),
	}
}

// ComparePair computes an Outcome from two already-executed RunResults.
// Returns ErrIncomparablePair if their ControlHash or ManifestHash differ.
func ComparePair(baseline, variant RunResult) (Outcome, error) {
	if baseline.ControlHash != variant.ControlHash {
		return Outcome{}, fmt.Errorf("%w: baseline control_hash=%s, variant control_hash=%s",
			ErrIncomparablePair, baseline.ControlHash, variant.ControlHash)
	}
	if baseline.ManifestHash != variant.ManifestHash {
		return Outcome{}, fmt.Errorf("%w: baseline manifest_hash=%s, variant manifest_hash=%s",
			ErrIncomparablePair, baseline.ManifestHash, variant.ManifestHash)
	}
	return compareRuns(baseline.PairID, baseline.ID, variant.ID,
		baseline.RawAlerts, variant.RawAlerts), nil
}

// ─── Mutations ────────────────────────────────────────────────────────────────

// applyMutation returns a deep-ish copy of sel with the mutation applied.
// The original sel is never modified (immutability of baseline input).
func applyMutation(
	sel *bank.BatchSelection,
	mtype MutationType,
	spec map[string]any,
) (*bank.BatchSelection, error) {
	// Start with a shallow copy; replace Transactions slice.
	mutated := *sel
	mutated.Transactions = make([]bank.Transaction, len(sel.Transactions))
	copy(mutated.Transactions, sel.Transactions)

	switch mtype {
	case MutationNone:
		// No change: variant = baseline (useful for regression / unchanged-input tests).
		return &mutated, nil

	case MutationSplitTransaction:
		return applySplitTransaction(&mutated, spec)

	case MutationDelayAndResubmit:
		return applyDelayAndResubmit(&mutated, spec)

	case MutationIntroduceShell:
		// Shell introduction requires adding entities/accounts not yet in the DB.
		// For now, we model this as removing all transactions above the structuring
		// threshold from a chosen entity, simulating smurfs handling the cash.
		return applyIntroduceShell(&mutated, spec)

	default:
		return nil, fmt.Errorf("unknown mutation_type: %q", mtype)
	}
}

// applySplitTransaction splits one large transaction from a chosen entity into
// N smaller ones, each just below the structuring threshold.
// Spec keys: "entity_index" (int, default 0), "split_count" (int, default 3),
//            "threshold_cents" (int64, default 1_000_000).
func applySplitTransaction(sel *bank.BatchSelection, spec map[string]any) (*bank.BatchSelection, error) {
	splitCount := intFromSpec(spec, "split_count", 3)
	thresholdCents := int64FromSpec(spec, "threshold_cents", 1_000_000)

	if splitCount < 2 {
		return nil, fmt.Errorf("split_transaction: split_count must be >= 2, got %d", splitCount)
	}

	// Find the first transaction at or above threshold.
	var result []bank.Transaction
	split := false
	for _, tx := range sel.Transactions {
		if !split && tx.AmountCents >= thresholdCents {
			// Replace with splitCount smaller transactions.
			partCents := (thresholdCents - 1) // just below threshold
			for i := 0; i < splitCount; i++ {
				part := tx // copy
				part.ID = deterministicVariantTxID(tx.ID, i)
				part.AmountCents = partCents
				result = append(result, part)
			}
			split = true
			continue
		}
		result = append(result, tx)
	}

	sel.Transactions = result
	return sel, nil
}

// applyDelayAndResubmit removes a chosen transaction and re-inserts it at a
// later timestamp (simulating delayed resubmission to avoid rapid-movement detection).
// Spec keys: "delay_minutes" (int, default 120).
func applyDelayAndResubmit(sel *bank.BatchSelection, spec map[string]any) (*bank.BatchSelection, error) {
	delayMinutes := intFromSpec(spec, "delay_minutes", 120)

	var result []bank.Transaction
	resubmitted := false
	for _, tx := range sel.Transactions {
		if !resubmitted {
			// Delay the first transaction by delayMinutes.
			newTS := tx.TxTimestamp.Add(time.Duration(delayMinutes) * time.Minute)
			if newTS.Before(sel.Manifest.WindowEnd) {
				tx.TxTimestamp = newTS
				tx.CreatedAt = newTS
				resubmitted = true
			}
		}
		result = append(result, tx)
	}
	sel.Transactions = result
	return sel, nil
}

// applyIntroduceShell removes large transactions from the first entity,
// simulating that those amounts are now handled by an intermediary shell.
// Spec keys: "threshold_cents" (int64, default 500_000).
func applyIntroduceShell(sel *bank.BatchSelection, spec map[string]any) (*bank.BatchSelection, error) {
	thresholdCents := int64FromSpec(spec, "threshold_cents", 500_000)

	if len(sel.Entities) == 0 {
		return sel, nil
	}

	// Build entity → accounts map.
	targetEntityID := sel.Entities[0].ID
	targetAccounts := make(map[uuid.UUID]bool)
	for _, a := range sel.Accounts {
		if a.EntityID == targetEntityID {
			targetAccounts[a.ID] = true
		}
	}

	// Remove large transactions from/to the target entity's accounts.
	var result []bank.Transaction
	for _, tx := range sel.Transactions {
		if (targetAccounts[tx.FromAccountID] || targetAccounts[tx.ToAccountID]) &&
			tx.AmountCents >= thresholdCents {
			continue // removed — would now flow through shell
		}
		result = append(result, tx)
	}
	sel.Transactions = result
	return sel, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func summariseAlerts(alerts []aml.Alert) map[aml.RuleID]int {
	m := make(map[aml.RuleID]int)
	for _, a := range alerts {
		m[a.RuleID]++
	}
	return m
}

func ruleIDSet(alerts []aml.Alert) map[aml.RuleID]bool {
	m := make(map[aml.RuleID]bool)
	for _, a := range alerts {
		m[a.RuleID] = true
	}
	return m
}

// deterministicVariantTxID produces a reproducible UUID for a split transaction
// part, derived from the original transaction ID and the part index.
func deterministicVariantTxID(origID uuid.UUID, partIdx int) uuid.UUID {
	// XOR the last byte with the part index to get a distinct but derived UUID.
	// This is simple and sufficient for synthetic data.
	var id uuid.UUID
	copy(id[:], origID[:])
	id[15] ^= byte(partIdx + 1)
	id[14] ^= byte((partIdx + 1) >> 8)
	return id
}

// hashManifest returns the SHA-256 hex string of the manifest JSON.
func hashManifest(m bank.CohortManifest) (string, error) {
	b, err := jsonMarshal(m)
	if err != nil {
		return "", err
	}
	sum := sha256Hex(b)
	return sum, nil
}

func intFromSpec(spec map[string]any, key string, def int) int {
	if spec == nil {
		return def
	}
	switch v := spec[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return def
}

func int64FromSpec(spec map[string]any, key string, def int64) int64 {
	if spec == nil {
		return def
	}
	switch v := spec[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	return def
}
