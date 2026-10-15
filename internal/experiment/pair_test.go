// Package experiment — pair_test.go
//
// Unit tests for the experiment pair executor.
// No database required; all tests are in-memory.
package experiment

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
	"github.com/yourorg/financial-crime-red-team/internal/bank"
)

// ─── Fixtures ─────────────────────────────────────────────────────────────────

var (
	pairWindowStart = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	pairWindowEnd   = pairWindowStart.Add(90 * 24 * time.Hour)
	fixedClock      = time.Date(2024, 4, 1, 12, 0, 0, 0, time.UTC)
)

// syntheticBatchSelection builds a BatchSelection from deterministic bank data.
// It uses Generate directly to get a fully consistent selection.
func syntheticBatchSelection(t *testing.T, seed int64) (*bank.BatchSelection, bank.CohortManifest) {
	t.Helper()
	cohortID := uuid.New()
	params := bank.GenerateParams{
		CohortID:    cohortID,
		Seed:        seed,
		Predicate:   bank.Predicate{},
		WindowStart: pairWindowStart,
		WindowEnd:   pairWindowEnd,
	}
	result, err := bank.Generate(params)
	if err != nil {
		t.Fatalf("bank.Generate: %v", err)
	}
	sel := &bank.BatchSelection{
		CohortID:     cohortID,
		WindowStart:  pairWindowStart,
		WindowEnd:    pairWindowEnd,
		Manifest:     result.Manifest,
		Entities:     result.Entities,
		Accounts:     result.Accounts,
		Transactions: result.Transactions,
	}
	return sel, result.Manifest
}

func defaultPairSpec(t *testing.T, sel *bank.BatchSelection, manifest bank.CohortManifest) PairSpec {
	t.Helper()
	input, err := NewInputContext(manifest, pairWindowStart, pairWindowEnd)
	if err != nil {
		t.Fatalf("NewInputContext: %v", err)
	}
	return PairSpec{
		Name:           "Test Pair",
		Input:          input,
		Config:         aml.DefaultControlConfig(),
		DeclaredChange: "Split high-value transactions below reporting threshold",
		MutationType:   MutationSplitTransaction,
		MutationSpec:   map[string]any{"split_count": 3, "threshold_cents": int64(100_000)},
	}
}

// ─── PairSpec.Validate ────────────────────────────────────────────────────────

func TestPairSpec_Validate_Valid(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 42)
	spec := defaultPairSpec(t, sel, manifest)
	if err := spec.Validate(); err != nil {
		t.Errorf("expected valid spec, got: %v", err)
	}
}

func TestPairSpec_Validate_EmptyName(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 42)
	spec := defaultPairSpec(t, sel, manifest)
	spec.Name = ""
	if err := spec.Validate(); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestPairSpec_Validate_NilCohortID(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 42)
	spec := defaultPairSpec(t, sel, manifest)
	spec.Input.CohortID = uuid.Nil
	if err := spec.Validate(); err == nil {
		t.Error("expected error for nil cohort_id")
	}
}

func TestPairSpec_Validate_EmptyManifestHash(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 42)
	spec := defaultPairSpec(t, sel, manifest)
	spec.Input.ManifestHash = ""
	if err := spec.Validate(); err == nil {
		t.Error("expected error for empty manifest_hash")
	}
}

func TestPairSpec_Validate_NilConfig(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 42)
	spec := defaultPairSpec(t, sel, manifest)
	spec.Config = nil
	if err := spec.Validate(); err == nil {
		t.Error("expected error for nil config")
	}
}

func TestPairSpec_Validate_EmptyDeclaredChange(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 42)
	spec := defaultPairSpec(t, sel, manifest)
	spec.DeclaredChange = ""
	if err := spec.Validate(); err == nil {
		t.Error("expected error for empty declared_change")
	}
}

func TestPairSpec_Validate_WindowEndBeforeStart(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 42)
	spec := defaultPairSpec(t, sel, manifest)
	spec.Input.WindowEnd = spec.Input.WindowStart.Add(-time.Hour)
	if err := spec.Validate(); err == nil {
		t.Error("expected error for window_end before window_start")
	}
}

// ─── Execute: determinism ─────────────────────────────────────────────────────

func TestExecute_Deterministic(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 99)
	spec := defaultPairSpec(t, sel, manifest)

	r1, err := Execute(spec, sel, fixedClock)
	if err != nil {
		t.Fatalf("Execute (1): %v", err)
	}
	r2, err := Execute(spec, sel, fixedClock)
	if err != nil {
		t.Fatalf("Execute (2): %v", err)
	}

	if r1.Baseline.AlertCount != r2.Baseline.AlertCount {
		t.Errorf("baseline alert count not deterministic: %d vs %d",
			r1.Baseline.AlertCount, r2.Baseline.AlertCount)
	}
	if r1.Variant.AlertCount != r2.Variant.AlertCount {
		t.Errorf("variant alert count not deterministic: %d vs %d",
			r1.Variant.AlertCount, r2.Variant.AlertCount)
	}
	if r1.Outcome.EvasionDelta != r2.Outcome.EvasionDelta {
		t.Errorf("evasion_delta not deterministic: %d vs %d",
			r1.Outcome.EvasionDelta, r2.Outcome.EvasionDelta)
	}
}

// ─── Execute: unchanged-input comparison (MutationNone) ──────────────────────

func TestExecute_UnchangedInput_IdenticalOutcome(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 77)
	input, err := NewInputContext(manifest, pairWindowStart, pairWindowEnd)
	if err != nil {
		t.Fatalf("NewInputContext: %v", err)
	}
	spec := PairSpec{
		Name:           "Unchanged Input Regression",
		Input:          input,
		Config:         aml.DefaultControlConfig(),
		DeclaredChange: "No change — regression baseline check",
		MutationType:   MutationNone,
		MutationSpec:   nil,
	}

	res, err := Execute(spec, sel, fixedClock)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !res.Outcome.OutcomeUnchanged {
		t.Errorf("expected OutcomeUnchanged=true for MutationNone, got EvasionDelta=%d new=%v dropped=%v",
			res.Outcome.EvasionDelta, res.Outcome.NewAlertRuleIDs, res.Outcome.DroppedAlertRuleIDs)
	}
	if res.Outcome.EvasionDelta != 0 {
		t.Errorf("expected zero evasion delta, got %d", res.Outcome.EvasionDelta)
	}
}

// ─── Execute: hashes carried through ─────────────────────────────────────────

func TestExecute_BothRunsCarrySameControlHash(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 55)
	spec := defaultPairSpec(t, sel, manifest)

	res, err := Execute(spec, sel, fixedClock)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if res.Baseline.ControlHash != spec.Config.ConfigHash {
		t.Errorf("baseline control_hash %q != config hash %q",
			res.Baseline.ControlHash, spec.Config.ConfigHash)
	}
	if res.Variant.ControlHash != spec.Config.ConfigHash {
		t.Errorf("variant control_hash %q != config hash %q",
			res.Variant.ControlHash, spec.Config.ConfigHash)
	}
	if res.Baseline.ControlHash != res.Variant.ControlHash {
		t.Error("baseline and variant control hashes differ")
	}
}

func TestExecute_BothRunsCarrySameManifestHash(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 55)
	spec := defaultPairSpec(t, sel, manifest)

	res, err := Execute(spec, sel, fixedClock)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if res.Baseline.ManifestHash != spec.Input.ManifestHash {
		t.Errorf("baseline manifest_hash mismatch")
	}
	if res.Variant.ManifestHash != spec.Input.ManifestHash {
		t.Errorf("variant manifest_hash mismatch")
	}
	if res.Baseline.ManifestHash != res.Variant.ManifestHash {
		t.Error("baseline and variant manifest hashes differ")
	}
}

func TestExecute_PairRecordsControlHash(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 55)
	spec := defaultPairSpec(t, sel, manifest)

	res, err := Execute(spec, sel, fixedClock)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if res.Pair.ControlHash != spec.Config.ConfigHash {
		t.Errorf("pair control_hash %q != config hash %q",
			res.Pair.ControlHash, spec.Config.ConfigHash)
	}
	if res.Pair.ManifestHash != spec.Input.ManifestHash {
		t.Errorf("pair manifest_hash %q != input hash %q",
			res.Pair.ManifestHash, spec.Input.ManifestHash)
	}
	if res.Pair.Seed != manifest.Seed {
		t.Errorf("pair seed %d != manifest seed %d", res.Pair.Seed, manifest.Seed)
	}
}

// ─── Execute: manifest hash mismatch rejection ───────────────────────────────

func TestExecute_RejectsManifestHashMismatch(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 100)
	spec := defaultPairSpec(t, sel, manifest)
	// Tamper the spec's manifest hash.
	spec.Input.ManifestHash = "deadbeef"

	_, err := Execute(spec, sel, fixedClock)
	if err == nil {
		t.Fatal("expected ErrIncomparablePair for manifest hash mismatch, got nil")
	}
}

// ─── Execute: invalid mutation type ──────────────────────────────────────────

func TestExecute_RejectsUnknownMutationType(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 111)
	input, err := NewInputContext(manifest, pairWindowStart, pairWindowEnd)
	if err != nil {
		t.Fatalf("NewInputContext: %v", err)
	}
	spec := PairSpec{
		Name:           "Bad Mutation",
		Input:          input,
		Config:         aml.DefaultControlConfig(),
		DeclaredChange: "Unknown mutation",
		MutationType:   MutationType("nonexistent_mutation"),
		MutationSpec:   nil,
	}
	_, err = Execute(spec, sel, fixedClock)
	if err == nil {
		t.Fatal("expected error for unknown mutation type, got nil")
	}
}

// ─── Execute: invalid PairSpec rejected ──────────────────────────────────────

func TestExecute_RejectsInvalidSpec(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 42)
	spec := defaultPairSpec(t, sel, manifest)
	spec.Name = "" // invalidate

	_, err := Execute(spec, sel, fixedClock)
	if err == nil {
		t.Fatal("expected validation error for empty name, got nil")
	}
}

// ─── ComparePair ──────────────────────────────────────────────────────────────

func TestComparePair_RejectsControlHashMismatch(t *testing.T) {
	cfg1 := aml.DefaultControlConfig()
	cfg2, _ := aml.NewControlConfig(
		aml.EnabledRules{aml.RuleStructuring},
		aml.StructuringConfig{ReportingThresholdCents: 2_000_000, WindowHours: 24, MinTransactions: 3},
		aml.LayeringConfig{MinHops: 3, WindowHours: 72},
		aml.RapidMovementConfig{MinAmountCents: 500_000, WindowMinutes: 60, DebitRatioPct: 80},
	)

	baseline := RunResult{ControlHash: cfg1.ConfigHash, ManifestHash: "abc"}
	variant := RunResult{ControlHash: cfg2.ConfigHash, ManifestHash: "abc"}

	_, err := ComparePair(baseline, variant)
	if err == nil {
		t.Fatal("expected ErrIncomparablePair for control hash mismatch")
	}
}

func TestComparePair_RejectsManifestHashMismatch(t *testing.T) {
	cfg := aml.DefaultControlConfig()
	baseline := RunResult{ControlHash: cfg.ConfigHash, ManifestHash: "aaa"}
	variant := RunResult{ControlHash: cfg.ConfigHash, ManifestHash: "bbb"}

	_, err := ComparePair(baseline, variant)
	if err == nil {
		t.Fatal("expected ErrIncomparablePair for manifest hash mismatch")
	}
}

func TestComparePair_IdenticalAlerts_Unchanged(t *testing.T) {
	cfg := aml.DefaultControlConfig()
	alerts := []aml.Alert{{ID: uuid.New(), RuleID: aml.RuleStructuring}}
	h := "samehash"

	baseline := RunResult{ControlHash: cfg.ConfigHash, ManifestHash: h, RawAlerts: alerts}
	variant := RunResult{ControlHash: cfg.ConfigHash, ManifestHash: h, RawAlerts: alerts}

	outcome, err := ComparePair(baseline, variant)
	if err != nil {
		t.Fatalf("ComparePair: %v", err)
	}
	if !outcome.OutcomeUnchanged {
		t.Error("expected OutcomeUnchanged=true for identical alerts")
	}
	if outcome.EvasionDelta != 0 {
		t.Errorf("expected zero evasion delta, got %d", outcome.EvasionDelta)
	}
}

func TestComparePair_DroppedRule_PositiveEvasionDelta(t *testing.T) {
	cfg := aml.DefaultControlConfig()
	h := "samehash"

	baseAlerts := []aml.Alert{
		{ID: uuid.New(), RuleID: aml.RuleStructuring},
		{ID: uuid.New(), RuleID: aml.RuleLayering},
	}
	varAlerts := []aml.Alert{} // evasion: all alerts dropped

	baseline := RunResult{ControlHash: cfg.ConfigHash, ManifestHash: h, RawAlerts: baseAlerts}
	variant := RunResult{ControlHash: cfg.ConfigHash, ManifestHash: h, RawAlerts: varAlerts}

	outcome, err := ComparePair(baseline, variant)
	if err != nil {
		t.Fatalf("ComparePair: %v", err)
	}
	if outcome.EvasionDelta != 2 {
		t.Errorf("expected evasion_delta=2, got %d", outcome.EvasionDelta)
	}
	if len(outcome.DroppedAlertRuleIDs) != 2 {
		t.Errorf("expected 2 dropped rules, got %v", outcome.DroppedAlertRuleIDs)
	}
	if outcome.OutcomeUnchanged {
		t.Error("expected OutcomeUnchanged=false when alerts dropped")
	}
}

func TestComparePair_NewRule_NegativeEvasionDelta(t *testing.T) {
	cfg := aml.DefaultControlConfig()
	h := "samehash"

	baseAlerts := []aml.Alert{}
	varAlerts := []aml.Alert{
		{ID: uuid.New(), RuleID: aml.RuleRapidMovement},
	}

	baseline := RunResult{ControlHash: cfg.ConfigHash, ManifestHash: h, RawAlerts: baseAlerts}
	variant := RunResult{ControlHash: cfg.ConfigHash, ManifestHash: h, RawAlerts: varAlerts}

	outcome, err := ComparePair(baseline, variant)
	if err != nil {
		t.Fatalf("ComparePair: %v", err)
	}
	if outcome.EvasionDelta != -1 {
		t.Errorf("expected evasion_delta=-1, got %d", outcome.EvasionDelta)
	}
	if len(outcome.NewAlertRuleIDs) != 1 {
		t.Errorf("expected 1 new rule, got %v", outcome.NewAlertRuleIDs)
	}
}

// ─── Outcome sorted rule IDs ──────────────────────────────────────────────────

func TestComparePair_DroppedRulesSorted(t *testing.T) {
	cfg := aml.DefaultControlConfig()
	h := "samehash"

	baseAlerts := []aml.Alert{
		{RuleID: aml.RuleRapidMovement},
		{RuleID: aml.RuleLayering},
		{RuleID: aml.RuleStructuring},
	}
	baseline := RunResult{ControlHash: cfg.ConfigHash, ManifestHash: h, RawAlerts: baseAlerts}
	variant := RunResult{ControlHash: cfg.ConfigHash, ManifestHash: h, RawAlerts: nil}

	outcome, _ := ComparePair(baseline, variant)
	for i := 1; i < len(outcome.DroppedAlertRuleIDs); i++ {
		if string(outcome.DroppedAlertRuleIDs[i]) < string(outcome.DroppedAlertRuleIDs[i-1]) {
			t.Errorf("dropped_rule_ids not sorted at index %d", i)
		}
	}
}

// ─── InputContext hash stability ──────────────────────────────────────────────

func TestNewInputContext_HashDeterministic(t *testing.T) {
	manifest := bank.CohortManifest{
		CohortID:         uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		Seed:             42,
		GeneratorVersion: bank.GeneratorVersion,
		EntityCount:      10,
		AccountCount:     25,
		TxCount:          200,
		WindowStart:      pairWindowStart,
		WindowEnd:        pairWindowEnd,
	}
	ctx1, _ := NewInputContext(manifest, pairWindowStart, pairWindowEnd)
	ctx2, _ := NewInputContext(manifest, pairWindowStart, pairWindowEnd)

	if ctx1.ManifestHash != ctx2.ManifestHash {
		t.Errorf("manifest hash not deterministic: %s vs %s", ctx1.ManifestHash, ctx2.ManifestHash)
	}
}

func TestNewInputContext_HashChangesWithManifest(t *testing.T) {
	m1 := bank.CohortManifest{CohortID: uuid.New(), Seed: 1, GeneratorVersion: bank.GeneratorVersion}
	m2 := bank.CohortManifest{CohortID: uuid.New(), Seed: 2, GeneratorVersion: bank.GeneratorVersion}

	ctx1, _ := NewInputContext(m1, pairWindowStart, pairWindowEnd)
	ctx2, _ := NewInputContext(m2, pairWindowStart, pairWindowEnd)

	if ctx1.ManifestHash == ctx2.ManifestHash {
		t.Error("different manifests produced the same hash")
	}
}

// ─── Reproducibility: same seed → identical pair results ─────────────────────

func TestExecute_Reproducibility_SameSeed(t *testing.T) {
	// Both runs must share the same CohortID so the manifest hash is identical.
	// The manifest embeds CohortID, so two uuid.New() calls would produce
	// different hashes even with the same seed.
	seed := int64(31415)
	fixedCohortID := uuid.MustParse("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee")

	params := bank.GenerateParams{
		CohortID:    fixedCohortID,
		Seed:        seed,
		Predicate:   bank.Predicate{},
		WindowStart: pairWindowStart,
		WindowEnd:   pairWindowEnd,
	}

	res1, err := bank.Generate(params)
	if err != nil {
		t.Fatalf("Generate (1): %v", err)
	}
	res2, err := bank.Generate(params) // identical params → identical output
	if err != nil {
		t.Fatalf("Generate (2): %v", err)
	}

	sel1 := &bank.BatchSelection{
		CohortID: fixedCohortID, WindowStart: pairWindowStart, WindowEnd: pairWindowEnd,
		Manifest: res1.Manifest, Entities: res1.Entities,
		Accounts: res1.Accounts, Transactions: res1.Transactions,
	}
	sel2 := &bank.BatchSelection{
		CohortID: fixedCohortID, WindowStart: pairWindowStart, WindowEnd: pairWindowEnd,
		Manifest: res2.Manifest, Entities: res2.Entities,
		Accounts: res2.Accounts, Transactions: res2.Transactions,
	}

	ctx1, err := NewInputContext(res1.Manifest, pairWindowStart, pairWindowEnd)
	if err != nil {
		t.Fatalf("NewInputContext (1): %v", err)
	}
	ctx2, err := NewInputContext(res2.Manifest, pairWindowStart, pairWindowEnd)
	if err != nil {
		t.Fatalf("NewInputContext (2): %v", err)
	}

	if ctx1.ManifestHash != ctx2.ManifestHash {
		t.Fatalf("same seed+cohortID produced different manifest hashes:\n  run1: %s\n  run2: %s",
			ctx1.ManifestHash, ctx2.ManifestHash)
	}

	makeSpec := func(ctx InputContext) PairSpec {
		return PairSpec{
			Name:           "Reproducibility Check",
			Input:          ctx,
			Config:         aml.DefaultControlConfig(),
			DeclaredChange: "Split large transactions",
			MutationType:   MutationSplitTransaction,
			MutationSpec:   map[string]any{"split_count": 3, "threshold_cents": int64(100_000)},
		}
	}

	r1, err := Execute(makeSpec(ctx1), sel1, fixedClock)
	if err != nil {
		t.Fatalf("Execute (1): %v", err)
	}
	r2, err := Execute(makeSpec(ctx2), sel2, fixedClock)
	if err != nil {
		t.Fatalf("Execute (2): %v", err)
	}

	if r1.Outcome.EvasionDelta != r2.Outcome.EvasionDelta {
		t.Errorf("evasion_delta not reproducible: %d vs %d",
			r1.Outcome.EvasionDelta, r2.Outcome.EvasionDelta)
	}
	if r1.Outcome.OutcomeUnchanged != r2.Outcome.OutcomeUnchanged {
		t.Errorf("OutcomeUnchanged not reproducible: %v vs %v",
			r1.Outcome.OutcomeUnchanged, r2.Outcome.OutcomeUnchanged)
	}
	if r1.Baseline.AlertCount != r2.Baseline.AlertCount {
		t.Errorf("baseline alert count not reproducible: %d vs %d",
			r1.Baseline.AlertCount, r2.Baseline.AlertCount)
	}
}

// ─── Mutation: SplitTransaction reduces large tx count ───────────────────────

func TestMutation_SplitTransaction_ReducesLargeTx(t *testing.T) {
	// Build a selection with one large transaction above threshold.
	cohortID := uuid.New()
	entityID := uuid.New()
	acct1 := uuid.New()
	acct2 := uuid.New()

	ts := pairWindowStart.Add(1 * time.Hour)
	manifest := bank.CohortManifest{CohortID: cohortID, Seed: 1, GeneratorVersion: bank.GeneratorVersion,
		WindowStart: pairWindowStart, WindowEnd: pairWindowEnd}

	sel := &bank.BatchSelection{
		CohortID:    cohortID,
		WindowStart: pairWindowStart,
		WindowEnd:   pairWindowEnd,
		Manifest:    manifest,
		Entities:    []bank.Entity{{ID: entityID, CohortID: cohortID}},
		Accounts: []bank.Account{
			{ID: acct1, CohortID: cohortID, EntityID: entityID},
			{ID: acct2, CohortID: cohortID, EntityID: entityID},
		},
		Transactions: []bank.Transaction{{
			ID: uuid.New(), CohortID: cohortID,
			FromAccountID: acct1, ToAccountID: acct2,
			AmountCents: 500_000, TxTimestamp: ts,
		}},
	}

	mutated, err := applyMutation(sel, MutationSplitTransaction, map[string]any{
		"split_count":    3,
		"threshold_cents": int64(100_000),
	})
	if err != nil {
		t.Fatalf("applyMutation: %v", err)
	}

	// Original had 1 tx; after split should have 3 (parts) replacing the 1.
	if len(mutated.Transactions) != 3 {
		t.Errorf("expected 3 split txs, got %d", len(mutated.Transactions))
	}
	for _, tx := range mutated.Transactions {
		if tx.AmountCents >= 100_000 {
			t.Errorf("split tx has amount %d >= threshold %d", tx.AmountCents, 100_000)
		}
	}
}

func TestMutation_SplitTransaction_InvalidSplitCount(t *testing.T) {
	sel := &bank.BatchSelection{Transactions: nil}
	_, err := applyMutation(sel, MutationSplitTransaction, map[string]any{"split_count": 1})
	if err == nil {
		t.Error("expected error for split_count < 2")
	}
}

// ─── Mutation: MutationNone leaves input unchanged ────────────────────────────

func TestMutation_None_LeavesTransactionsUnchanged(t *testing.T) {
	sel, manifest := syntheticBatchSelection(t, 7)
	originalCount := len(sel.Transactions)
	_ = manifest

	mutated, err := applyMutation(sel, MutationNone, nil)
	if err != nil {
		t.Fatalf("applyMutation(MutationNone): %v", err)
	}
	if len(mutated.Transactions) != originalCount {
		t.Errorf("MutationNone changed tx count: %d → %d", originalCount, len(mutated.Transactions))
	}
}

// ─── ErrInvalidPairSpec sentinel ─────────────────────────────────────────────

func TestErrInvalidPairSpec_IsError(t *testing.T) {
	if ErrInvalidPairSpec.Error() == "" {
		t.Error("ErrInvalidPairSpec.Error() returned empty string")
	}
}

func TestErrIncomparablePair_IsError(t *testing.T) {
	if ErrIncomparablePair.Error() == "" {
		t.Error("ErrIncomparablePair.Error() returned empty string")
	}
}
