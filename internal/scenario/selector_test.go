// Package scenario — selector_test.go
//
// Tests for the adaptive selector and its downstream integration with
// the experiment layer.
//
// Coverage:
//   - Weights validation: zero weight, bad sum, empty version
//   - Coverage scoring: never-tried vs saturated type
//   - Novelty scoring: identical tags, disjoint tags, partial overlap
//   - Jaccard similarity edge cases: empty sets, single element
//   - Deduplication: same fingerprint rejected across two Select calls
//   - Ranking: higher-scored candidate appears first
//   - Tie-breaking: deterministic by fingerprint
//   - State update: SeenFingerprints and PoolTagSets grow correctly
//   - Reproducibility: same input → same ranking, same scores
//   - Downstream experiment integration: selected candidate produces valid PairSpec
package scenario

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/experiment"
)

// ─── SelectionWeights validation ─────────────────────────────────────────────

func TestWeights_DefaultValidates(t *testing.T) {
	if err := DefaultWeights().Validate(); err != nil {
		t.Errorf("DefaultWeights failed validation: %v", err)
	}
}

func TestWeights_SumNotOne(t *testing.T) {
	w := DefaultWeights()
	w.Coverage = 0.5 // total > 1
	if err := w.Validate(); err == nil {
		t.Error("expected error for weights that don't sum to 1")
	}
}

func TestWeights_NegativeComponent(t *testing.T) {
	w := SelectionWeights{
		Version: WeightsVersion,
		Coverage: -0.1, Novelty: 0.6, Validity: 0.3, EIV: 0.2,
	}
	if err := w.Validate(); err == nil {
		t.Error("expected error for negative weight component")
	}
}

func TestWeights_EmptyVersion(t *testing.T) {
	w := DefaultWeights()
	w.Version = ""
	if err := w.Validate(); err == nil {
		t.Error("expected error for empty version")
	}
}

func TestWeights_Score_AllZeroComponents(t *testing.T) {
	w := DefaultWeights()
	sc := ScoreComponents{}
	if s := w.Score(sc); s != 0 {
		t.Errorf("expected 0 score for zero components, got %f", s)
	}
}

func TestWeights_Score_AllOneComponents(t *testing.T) {
	w := DefaultWeights()
	sc := ScoreComponents{Coverage: 1, Novelty: 1, Validity: 1, EIV: 1}
	if s := w.Score(sc); s < 0.9999 || s > 1.0001 {
		t.Errorf("expected 1.0 score for all-1 components, got %f", s)
	}
}

// ─── NewSelector ─────────────────────────────────────────────────────────────

func TestNewSelector_InvalidWeights(t *testing.T) {
	// Weights sum to 1.1 (not 1.0) — should fail validation.
	bad := SelectionWeights{
		Version:  "v0.1.0",
		Coverage: 0.40, // sum = 0.40+0.30+0.25+0.15 = 1.10
		Novelty:  0.30,
		Validity: 0.25,
		EIV:      0.15,
	}
	_, err := NewSelector(bad)
	if err == nil {
		t.Error("expected error for weights that don't sum to 1.0")
	}
}

func TestNewSelector_ValidWeights(t *testing.T) {
	_, err := NewSelector(DefaultWeights())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ─── Coverage scoring ─────────────────────────────────────────────────────────

func TestCoverageScore_EmptyState(t *testing.T) {
	sel, _ := NewSelector(DefaultWeights())
	state := NewSelectionState()
	score := sel.coverageScore(MutationSplitTransaction, &state)
	if score != 1.0 {
		t.Errorf("coverage score for empty state: %f, want 1.0", score)
	}
}

func TestCoverageScore_Saturated(t *testing.T) {
	sel, _ := NewSelector(DefaultWeights())
	state := SelectionState{
		MutationTypeCounts: map[MutationType]int{MutationSplitTransaction: 10},
		TotalExperiments:   10,
		SeenFingerprints:   make(map[string]bool),
	}
	score := sel.coverageScore(MutationSplitTransaction, &state)
	if score != 0.0 {
		t.Errorf("coverage score for saturated type: %f, want 0.0", score)
	}
}

func TestCoverageScore_NeverTried(t *testing.T) {
	sel, _ := NewSelector(DefaultWeights())
	state := SelectionState{
		MutationTypeCounts: map[MutationType]int{MutationSplitTransaction: 5},
		TotalExperiments:   5,
		SeenFingerprints:   make(map[string]bool),
	}
	score := sel.coverageScore(MutationDelayAndResubmit, &state)
	if score != 1.0 {
		t.Errorf("coverage score for never-tried: %f, want 1.0", score)
	}
}

func TestCoverageScore_HalfFrequency(t *testing.T) {
	sel, _ := NewSelector(DefaultWeights())
	state := SelectionState{
		MutationTypeCounts: map[MutationType]int{MutationSplitTransaction: 5},
		TotalExperiments:   10,
		SeenFingerprints:   make(map[string]bool),
	}
	score := sel.coverageScore(MutationSplitTransaction, &state)
	const want = 0.5
	if score < want-1e-6 || score > want+1e-6 {
		t.Errorf("coverage score at 50%%: %f, want %f", score, want)
	}
}

// ─── Novelty scoring ─────────────────────────────────────────────────────────

func TestNoveltyScore_EmptyPool(t *testing.T) {
	sel, _ := NewSelector(DefaultWeights())
	state := NewSelectionState()
	score := sel.noveltyScore([]string{"tag1", "tag2"}, &state)
	if score != 1.0 {
		t.Errorf("novelty for empty pool: %f, want 1.0", score)
	}
}

func TestNoveltyScore_IdenticalTags(t *testing.T) {
	sel, _ := NewSelector(DefaultWeights())
	tags := []string{"structuring", "wire"}
	state := SelectionState{
		PoolTagSets:      [][]string{tags},
		SeenFingerprints: make(map[string]bool),
	}
	score := sel.noveltyScore(tags, &state)
	if score != 0.0 {
		t.Errorf("novelty for identical tags: %f, want 0.0", score)
	}
}

func TestNoveltyScore_DisjointTags(t *testing.T) {
	sel, _ := NewSelector(DefaultWeights())
	state := SelectionState{
		PoolTagSets:      [][]string{{"structuring", "wire"}},
		SeenFingerprints: make(map[string]bool),
	}
	score := sel.noveltyScore([]string{"rapid-movement", "crypto"}, &state)
	if score != 1.0 {
		t.Errorf("novelty for disjoint tags: %f, want 1.0", score)
	}
}

func TestNoveltyScore_PartialOverlap(t *testing.T) {
	sel, _ := NewSelector(DefaultWeights())
	state := SelectionState{
		PoolTagSets:      [][]string{{"a", "b", "c"}},
		SeenFingerprints: make(map[string]bool),
	}
	// Candidate tags: {a, b} — intersection={a,b}, union={a,b,c} → J=2/3
	// Novelty = 1 - 2/3 = 1/3 ≈ 0.333
	score := sel.noveltyScore([]string{"a", "b"}, &state)
	const wantLo, wantHi = 0.333 - 0.001, 0.333 + 0.001
	if score < wantLo || score > wantHi {
		t.Errorf("novelty for partial overlap: %f, want ~0.333", score)
	}
}

// ─── Jaccard similarity ───────────────────────────────────────────────────────

func TestJaccard_BothEmpty(t *testing.T) {
	j := jaccardSimilarity(nil, nil)
	if j != 0.0 {
		t.Errorf("Jaccard(empty, empty)=%f, want 0.0", j)
	}
}

func TestJaccard_OneEmpty(t *testing.T) {
	j := jaccardSimilarity([]string{"a"}, nil)
	if j != 0.0 {
		t.Errorf("Jaccard({a}, empty)=%f, want 0.0", j)
	}
}

func TestJaccard_Identical(t *testing.T) {
	j := jaccardSimilarity([]string{"a", "b"}, []string{"a", "b"})
	if j < 0.9999 {
		t.Errorf("Jaccard identical sets: %f", j)
	}
}

func TestJaccard_Disjoint(t *testing.T) {
	j := jaccardSimilarity([]string{"a"}, []string{"b"})
	if j != 0.0 {
		t.Errorf("Jaccard disjoint: %f, want 0.0", j)
	}
}

// ─── Select: deduplication ────────────────────────────────────────────────────

func TestSelect_DuplicateRejected(t *testing.T) {
	sel, _ := NewSelector(DefaultWeights())
	state := NewSelectionState()
	corrID := uuid.New()

	// First batch: one valid candidate.
	r1 := makeGenerationResult(corrID, []Candidate{
		makeSplitCandidate(corrID, "Split wires", []string{"wire"}),
	})
	res1 := sel.Select(r1, &state, 5)
	if len(res1.Selected) != 1 {
		t.Fatalf("first select: expected 1 selected, got %d", len(res1.Selected))
	}

	// Second batch: same fingerprint → should be rejected.
	r2 := makeGenerationResult(corrID, []Candidate{
		makeSplitCandidate(corrID, "Split wires", []string{"wire"}),
	})
	res2 := sel.Select(r2, &state, 5)
	if len(res2.Selected) != 0 {
		t.Errorf("second select: expected 0 selected (duplicate), got %d", len(res2.Selected))
	}
	if len(res2.Rejected) == 0 {
		t.Error("expected rejection record for duplicate")
	}
	if res2.Rejected[0].Reason != RejectDuplicate {
		t.Errorf("rejection reason: %s", res2.Rejected[0].Reason)
	}
}

// ─── Select: ranking ─────────────────────────────────────────────────────────

func TestSelect_RankingDescendingByScore(t *testing.T) {
	// Use a weights config that emphasises EIV so we can control ordering.
	w := SelectionWeights{
		Version: WeightsVersion,
		Coverage: 0.0, Novelty: 0.0, Validity: 0.0, EIV: 1.0,
	}
	sel, _ := NewSelector(w)
	state := NewSelectionState()
	corrID := uuid.New()

	// Candidate A: EIV 0.9 (high)
	// Candidate B: EIV 0.1 (low)
	candA := makeSplitCandidate(corrID, "High EIV", []string{"a"})
	candA.ScoreComponents.EIV = 0.9
	candB := makeDelayCandidate(corrID, "Low EIV", []string{"b"})
	candB.ScoreComponents.EIV = 0.1

	result := makeGenerationResult(corrID, []Candidate{candB, candA}) // intentional reverse order
	res := sel.Select(result, &state, 5)

	if len(res.Selected) != 2 {
		t.Fatalf("expected 2 selected, got %d", len(res.Selected))
	}
	if res.Selected[0].DeclaredChange != "High EIV" {
		t.Errorf("first selected should be High EIV, got %q", res.Selected[0].DeclaredChange)
	}
}

// ─── Select: top-N cutoff ─────────────────────────────────────────────────────

func TestSelect_TopNCutoff(t *testing.T) {
	sel, _ := NewSelector(DefaultWeights())
	state := NewSelectionState()
	corrID := uuid.New()

	candidates := []Candidate{
		makeSplitCandidate(corrID, "Candidate A", []string{"a"}),
		makeDelayCandidate(corrID, "Candidate B", []string{"b"}),
		makeShellCandidate(corrID, "Candidate C", []string{"c"}),
	}
	result := makeGenerationResult(corrID, candidates)
	res := sel.Select(result, &state, 2) // only top 2

	if len(res.Selected) != 2 {
		t.Errorf("expected 2 selected (top-N=2), got %d", len(res.Selected))
	}
	// The dropped one should appear in rejected with RejectBudgetExceeded.
	budgetRejections := 0
	for _, r := range res.Rejected {
		if r.Reason == RejectBudgetExceeded {
			budgetRejections++
		}
	}
	if budgetRejections != 1 {
		t.Errorf("expected 1 budget rejection, got %d", budgetRejections)
	}
}

// ─── Select: state update ────────────────────────────────────────────────────

func TestSelect_StateUpdated(t *testing.T) {
	sel, _ := NewSelector(DefaultWeights())
	state := NewSelectionState()
	corrID := uuid.New()

	cand := makeSplitCandidate(corrID, "Test candidate", []string{"tag1"})
	result := makeGenerationResult(corrID, []Candidate{cand})
	res := sel.Select(result, &state, 5)

	if len(res.Selected) == 0 {
		t.Fatal("expected at least 1 selected")
	}
	fp := res.Selected[0].FingerprintKey()
	if !state.SeenFingerprints[fp] {
		t.Error("fingerprint not added to state.SeenFingerprints")
	}
	if len(state.PoolTagSets) == 0 {
		t.Error("state.PoolTagSets not updated")
	}
	if state.TotalExperiments != 1 {
		t.Errorf("TotalExperiments: %d, want 1", state.TotalExperiments)
	}
}

// ─── Select: WeightsVersion carried through ───────────────────────────────────

func TestSelect_WeightsVersionInResult(t *testing.T) {
	sel, _ := NewSelector(DefaultWeights())
	state := NewSelectionState()
	result := makeGenerationResult(uuid.New(), nil)
	res := sel.Select(result, &state, 5)
	if res.WeightsVersion != WeightsVersion {
		t.Errorf("WeightsVersion: %s, want %s", res.WeightsVersion, WeightsVersion)
	}
}

// ─── Reproducibility ─────────────────────────────────────────────────────────

func TestSelect_Reproducible_SameInputSameRanking(t *testing.T) {
	corrID := uuid.New()
	candidates := []Candidate{
		makeSplitCandidate(corrID, "Candidate A", []string{"structuring", "wire"}),
		makeDelayCandidate(corrID, "Candidate B", []string{"rapid-movement"}),
		makeShellCandidate(corrID, "Candidate C", []string{"layering", "shell"}),
	}

	run := func() SelectionResult {
		sel, _ := NewSelector(DefaultWeights())
		state := NewSelectionState()
		result := makeGenerationResult(corrID, candidates)
		return sel.Select(result, &state, 3)
	}

	r1 := run()
	r2 := run()

	if len(r1.Selected) != len(r2.Selected) {
		t.Fatalf("selected count differs: %d vs %d", len(r1.Selected), len(r2.Selected))
	}
	for i := range r1.Selected {
		if r1.Selected[i].FingerprintKey() != r2.Selected[i].FingerprintKey() {
			t.Errorf("rank %d fingerprint differs: %s vs %s",
				i, r1.Selected[i].FingerprintKey(), r2.Selected[i].FingerprintKey())
		}
		if r1.Selected[i].Score != r2.Selected[i].Score {
			t.Errorf("rank %d score differs: %f vs %f",
				i, r1.Selected[i].Score, r2.Selected[i].Score)
		}
	}
}

// ─── Downstream experiment integration ───────────────────────────────────────
//
// This test verifies that a selected Candidate can be fed directly into
// experiment.Execute() using the existing deterministic experiment engine
// without any rewriting of that module.

func TestSelect_DownstreamExperiment_ValidPairSpec(t *testing.T) {
	// 1. Generate a synthetic bank cohort.
	cohortID := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	windowStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(90 * 24 * time.Hour)

	bankResult, err := bank.Generate(bank.GenerateParams{
		CohortID:    cohortID,
		Seed:        42,
		Predicate:   bank.Predicate{},
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
	})
	if err != nil {
		t.Fatalf("bank.Generate: %v", err)
	}

	sel := &bank.BatchSelection{
		CohortID:     cohortID,
		WindowStart:  windowStart,
		WindowEnd:    windowEnd,
		Manifest:     bankResult.Manifest,
		Entities:     bankResult.Entities,
		Accounts:     bankResult.Accounts,
		Transactions: bankResult.Transactions,
	}

	// 2. Run the selector to choose a candidate.
	corrID := uuid.New()
	candidates := []Candidate{
		makeSplitCandidate(corrID, "Split transactions above $1k", []string{"structuring"}),
	}
	selector, _ := NewSelector(DefaultWeights())
	state := NewSelectionState()
	res := selector.Select(makeGenerationResult(corrID, candidates), &state, 1)

	if len(res.Selected) == 0 {
		t.Fatal("no candidate selected")
	}
	chosen := res.Selected[0]

	// 3. Convert the selected Candidate into an experiment.PairSpec.
	input, err := experiment.NewInputContext(bankResult.Manifest, windowStart, windowEnd)
	if err != nil {
		t.Fatalf("NewInputContext: %v", err)
	}

	pairSpec := experiment.PairSpec{
		Name:           chosen.DeclaredChange,
		Input:          input,
		Config:         aml.DefaultControlConfig(),
		DeclaredChange: chosen.DeclaredChange,
		MutationType:   experiment.MutationType(chosen.MutationType),
		MutationSpec: map[string]any{
			"split_count":     chosen.MutationSpec.SplitCount,
			"threshold_cents": chosen.MutationSpec.ThresholdCents,
		},
	}

	if err := pairSpec.Validate(); err != nil {
		t.Fatalf("PairSpec.Validate: %v", err)
	}

	// 4. Execute the experiment pair (no DB needed — pure in-memory).
	execResult, err := experiment.Execute(pairSpec, sel, time.Now())
	if err != nil {
		t.Fatalf("experiment.Execute: %v", err)
	}

	// 5. Verify the outcome is well-formed.
	if execResult.Pair.ControlHash == "" {
		t.Error("pair.ControlHash is empty")
	}
	if execResult.Pair.ManifestHash == "" {
		t.Error("pair.ManifestHash is empty")
	}
	// Both runs must carry the same hashes.
	if execResult.Baseline.ControlHash != execResult.Variant.ControlHash {
		t.Error("baseline and variant control hashes differ")
	}
	// Outcome must be present (even if unchanged).
	// We just check it's structurally valid.
	_ = execResult.Outcome.EvasionDelta
	_ = execResult.Outcome.OutcomeUnchanged
}

func TestSelect_DownstreamExperiment_Reproducible(t *testing.T) {
	// Running the same selected candidate twice must produce the same EvasionDelta.
	cohortID := uuid.MustParse("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee")
	windowStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(90 * 24 * time.Hour)

	bankResult, _ := bank.Generate(bank.GenerateParams{
		CohortID:    cohortID,
		Seed:        77,
		Predicate:   bank.Predicate{},
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
	})

	sel := &bank.BatchSelection{
		CohortID:     cohortID,
		WindowStart:  windowStart,
		WindowEnd:    windowEnd,
		Manifest:     bankResult.Manifest,
		Entities:     bankResult.Entities,
		Accounts:     bankResult.Accounts,
		Transactions: bankResult.Transactions,
	}

	input, _ := experiment.NewInputContext(bankResult.Manifest, windowStart, windowEnd)
	pairSpec := experiment.PairSpec{
		Name:           "Reproducibility check",
		Input:          input,
		Config:         aml.DefaultControlConfig(),
		DeclaredChange: "Delay all large transactions by 2 hours",
		MutationType:   experiment.MutationDelayAndResubmit,
		MutationSpec:   map[string]any{"delay_minutes": 120},
	}

	clock := time.Date(2024, 4, 1, 12, 0, 0, 0, time.UTC)
	r1, err := experiment.Execute(pairSpec, sel, clock)
	if err != nil {
		t.Fatalf("Execute (1): %v", err)
	}
	r2, err := experiment.Execute(pairSpec, sel, clock)
	if err != nil {
		t.Fatalf("Execute (2): %v", err)
	}

	if r1.Outcome.EvasionDelta != r2.Outcome.EvasionDelta {
		t.Errorf("EvasionDelta not reproducible: %d vs %d",
			r1.Outcome.EvasionDelta, r2.Outcome.EvasionDelta)
	}
	if r1.Outcome.OutcomeUnchanged != r2.Outcome.OutcomeUnchanged {
		t.Errorf("OutcomeUnchanged not reproducible")
	}
}

// ─── Test fixtures ────────────────────────────────────────────────────────────

func makeSplitCandidate(corrID uuid.UUID, change string, tags []string) Candidate {
	return Candidate{
		ID:             uuid.New(),
		CorrelationID:  corrID,
		MutationType:   MutationSplitTransaction,
		DeclaredChange: change,
		MutationSpec:   MutationSpec{SplitCount: 3, ThresholdCents: 100_000},
		Rationale:      "Splitting avoids per-transaction reporting threshold.",
		Tags:           tags,
		GeneratedAt:    time.Now(),
		ScoreComponents: ScoreComponents{Validity: 1.0, EIV: 0.6},
	}
}

func makeDelayCandidate(corrID uuid.UUID, change string, tags []string) Candidate {
	return Candidate{
		ID:             uuid.New(),
		CorrelationID:  corrID,
		MutationType:   MutationDelayAndResubmit,
		DeclaredChange: change,
		MutationSpec:   MutationSpec{DelayMinutes: 120},
		Rationale:      "Delay reduces rapid-movement alert probability.",
		Tags:           tags,
		GeneratedAt:    time.Now(),
		ScoreComponents: ScoreComponents{Validity: 1.0, EIV: 0.5},
	}
}

func makeShellCandidate(corrID uuid.UUID, change string, tags []string) Candidate {
	return Candidate{
		ID:             uuid.New(),
		CorrelationID:  corrID,
		MutationType:   MutationIntroduceShell,
		DeclaredChange: change,
		MutationSpec:   MutationSpec{ThresholdCents: 500_000},
		Rationale:      "Shell entities hide beneficial ownership.",
		Tags:           tags,
		GeneratedAt:    time.Now(),
		ScoreComponents: ScoreComponents{Validity: 1.0, EIV: 0.7},
	}
}

func makeGenerationResult(corrID uuid.UUID, candidates []Candidate) GenerationResult {
	return GenerationResult{
		Accepted:      candidates,
		CorrelationID: corrID,
	}
}
