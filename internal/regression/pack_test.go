package regression

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/experiment"
)

var (
	winStart = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	winEnd   = time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
)

// synthBatch generates a deterministic batch for a fixed cohort ID + seed.
func synthBatch(t *testing.T, cohortID uuid.UUID, seed int64) *bank.BatchSelection {
	t.Helper()
	res, err := bank.Generate(bank.GenerateParams{
		CohortID:    cohortID,
		Seed:        seed,
		Predicate:   bank.Predicate{},
		WindowStart: winStart,
		WindowEnd:   winEnd,
	})
	if err != nil {
		t.Fatalf("bank.Generate: %v", err)
	}
	return &bank.BatchSelection{
		CohortID:     cohortID,
		WindowStart:  winStart,
		WindowEnd:    winEnd,
		Manifest:     res.Manifest,
		Entities:     res.Entities,
		Accounts:     res.Accounts,
		Transactions: res.Transactions,
	}
}

// deriveCase executes an experiment and returns a Case capturing its outcome.
func deriveCase(t *testing.T, cohortID uuid.UUID, seed int64) (Case, BatchProvider) {
	t.Helper()
	sel := synthBatch(t, cohortID, seed)

	inputCtx, err := experiment.NewInputContext(sel.Manifest, winStart, winEnd)
	if err != nil {
		t.Fatalf("NewInputContext: %v", err)
	}
	cfg := aml.DefaultControlConfig()
	spec := experiment.PairSpec{
		Name:           "Regression Test Case",
		Input:          inputCtx,
		Config:         cfg,
		DeclaredChange: "Split high-value tx below threshold",
		MutationType:   experiment.MutationSplitTransaction,
		MutationSpec:   map[string]any{"split_count": 3, "threshold_cents": int64(100_000)},
	}
	res, err := experiment.Execute(spec, sel, winStart)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	c := Case{
		ExperimentID:   uuid.New(),
		Name:           spec.Name,
		CohortID:       cohortID,
		ManifestHash:   inputCtx.ManifestHash,
		ControlHash:    cfg.ConfigHash,
		WindowStart:    winStart,
		WindowEnd:      winEnd,
		DeclaredChange: spec.DeclaredChange,
		MutationType:   string(spec.MutationType),
		MutationSpec:   spec.MutationSpec,
		Expected: ExpectedDiff{
			EvasionDelta:        res.Outcome.EvasionDelta,
			NewAlertRuleIDs:     ruleIDsToStrings(res.Outcome.NewAlertRuleIDs),
			DroppedAlertRuleIDs: ruleIDsToStrings(res.Outcome.DroppedAlertRuleIDs),
			OutcomeUnchanged:    res.Outcome.OutcomeUnchanged,
		},
		SourceChecksum: "deadbeef",
	}

	provider := func(_ context.Context, c Case) (*bank.BatchSelection, error) {
		return synthBatch(t, c.CohortID, seed), nil
	}
	return c, provider
}

// ─── Pack build / checksum / export / import ──────────────────────────────────

func TestBuildComputeChecksum(t *testing.T) {
	cohortID := uuid.New()
	c, _ := deriveCase(t, cohortID, 42)
	p, err := Build([]Case{c}, aml.ControlVersion, time.Now())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if p.Checksum == "" {
		t.Fatal("checksum empty")
	}
	ok, err := p.VerifyChecksum()
	if err != nil || !ok {
		t.Fatalf("VerifyChecksum: ok=%v err=%v", ok, err)
	}
}

func TestBuildSortsCases(t *testing.T) {
	c1 := Case{Name: "zebra", ExperimentID: uuid.New()}
	c2 := Case{Name: "alpha", ExperimentID: uuid.New()}
	p, err := Build([]Case{c1, c2}, "v0.1.0", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if p.Cases[0].Name != "alpha" {
		t.Errorf("cases not sorted, first = %q", p.Cases[0].Name)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	cohortID := uuid.New()
	c, _ := deriveCase(t, cohortID, 7)
	p, err := Build([]Case{c}, aml.ControlVersion, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := p.Export(&buf); err != nil {
		t.Fatalf("Export: %v", err)
	}

	got, err := Import(&buf)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if got.Checksum != p.Checksum {
		t.Errorf("checksum drift: %s != %s", got.Checksum, p.Checksum)
	}
	if len(got.Cases) != 1 || got.Cases[0].Name != c.Name {
		t.Errorf("case not preserved: %+v", got.Cases)
	}
}

func TestImportRejectsTamperedChecksum(t *testing.T) {
	c := Case{Name: "x", ExperimentID: uuid.New()}
	p, _ := Build([]Case{c}, "v0.1.0", time.Now())
	p.Cases[0].Name = "tampered" // mutate after checksum computed

	var buf bytes.Buffer
	_ = p.Export(&buf)
	if _, err := Import(&buf); err == nil {
		t.Fatal("expected checksum mismatch error for tampered pack")
	}
}

func TestImportRejectsBadVersion(t *testing.T) {
	p := Pack{Version: "9.9", Cases: []Case{}}
	var buf bytes.Buffer
	_ = p.Export(&buf)
	if _, err := Import(&buf); err == nil {
		t.Fatal("expected unsupported version error")
	}
}

func TestImportRejectsUnknownFields(t *testing.T) {
	raw := `{"version":"1.0","amazon_prime":true}`
	if _, err := Import(bytes.NewBufferString(raw)); err == nil {
		t.Fatal("expected error for unknown fields")
	}
}

// ─── Comparison ───────────────────────────────────────────────────────────────

func TestCompareCasePass(t *testing.T) {
	c := Case{
		Name: "c",
		Expected: ExpectedDiff{
			EvasionDelta:     3,
			NewAlertRuleIDs:  []string{"R1"},
			OutcomeUnchanged: false,
		},
	}
	actual := ExpectedDiff{EvasionDelta: 3, NewAlertRuleIDs: []string{"R1"}}
	res := CompareCase(c, actual)
	if !res.Passed {
		t.Errorf("expected pass, mismatches: %v", res.Mismatches)
	}
}

func TestCompareCaseRuleOrderInsensitive(t *testing.T) {
	c := Case{Expected: ExpectedDiff{NewAlertRuleIDs: []string{"R2", "R1"}}}
	actual := ExpectedDiff{NewAlertRuleIDs: []string{"R1", "R2"}}
	if !CompareCase(c, actual).Passed {
		t.Error("rule ID comparison should be order-insensitive")
	}
}

func TestCompareCaseDeltaMismatch(t *testing.T) {
	c := Case{Expected: ExpectedDiff{EvasionDelta: 3}}
	actual := ExpectedDiff{EvasionDelta: 5}
	res := CompareCase(c, actual)
	if res.Passed {
		t.Error("expected failure on delta mismatch")
	}
	if len(res.Mismatches) == 0 {
		t.Error("expected a mismatch description")
	}
}

func TestCompareCaseUnchangedMismatch(t *testing.T) {
	c := Case{Expected: ExpectedDiff{OutcomeUnchanged: true}}
	actual := ExpectedDiff{OutcomeUnchanged: false}
	if CompareCase(c, actual).Passed {
		t.Error("expected failure on outcome_unchanged mismatch")
	}
}

// ─── Runner ───────────────────────────────────────────────────────────────────

func TestRunCasePasses(t *testing.T) {
	cohortID := uuid.New()
	c, provider := deriveCase(t, cohortID, 42)
	res, err := RunCase(context.Background(), c, provider)
	if err != nil {
		t.Fatalf("RunCase: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected deterministic replay to pass, mismatches: %v", res.Mismatches)
	}
}

func TestRunPackAllPass(t *testing.T) {
	cohortID := uuid.New()
	c1, p1 := deriveCase(t, cohortID, 1)
	c2, _ := deriveCase(t, cohortID, 2)

	pack, err := Build([]Case{c1, c2}, aml.ControlVersion, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// Provider uses the same seed as each case's cohort; both were built with
	// cohortID but different seeds. Rebuild with matching seeds.
	provider := func(_ context.Context, c Case) (*bank.BatchSelection, error) {
		seed := int64(1)
		if c.Name == c2.Name && c.ExperimentID == c2.ExperimentID {
			seed = 2
		}
		return synthBatch(t, c.CohortID, seed), nil
	}
	_ = p1

	res, err := RunPack(context.Background(), pack, provider)
	if err != nil {
		t.Fatalf("RunPack: %v", err)
	}
	if res.Total != 2 {
		t.Fatalf("total = %d, want 2", res.Total)
	}
	if !res.OK() {
		t.Errorf("expected all pass, got %d failed: %+v", res.Failed, res.Results)
	}
}

func TestRunCaseControlHashDrift(t *testing.T) {
	cohortID := uuid.New()
	c, provider := deriveCase(t, cohortID, 42)
	c.ControlHash = "stale-hash-from-old-config"
	res, err := RunCase(context.Background(), c, provider)
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed {
		t.Error("expected control-hash drift to fail the case")
	}
}
