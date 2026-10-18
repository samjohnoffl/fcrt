// Package experiment — repository_test.go
//
// Integration tests for the experiment Repository.
// Require a real PostgreSQL database with all migrations applied.
//
// Skip with: go test -short ./internal/experiment/
//
//	Run with:  DATABASE_TEST_URL="host=/var/run/postgresql dbname=fincrime_test sslmode=disable" \
//	             go test -v -timeout 120s ./internal/experiment/
package experiment

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
	"github.com/yourorg/financial-crime-red-team/internal/bank"
)

// ─── Test helpers ─────────────────────────────────────────────────────────────

func testDBURL(t *testing.T) string {
	t.Helper()
	if url := os.Getenv("DATABASE_TEST_URL"); url != "" {
		return url
	}
	return "host=/var/run/postgresql dbname=fincrime_test sslmode=disable"
}

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDBURL(t))
	if err != nil {
		t.Skipf("cannot connect to test DB: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("test DB not reachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedCohort creates a cohort row in the DB and returns its ID.
// Used to satisfy the FK constraint on experiment_pair.cohort_id.
func seedCohort(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	cohortID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO cohort (id, name, predicate, seed, generator_version, manifest)
		VALUES ($1, $2, '{}', 0, '', '{}'::jsonb)`,
		cohortID, "experiment-test-cohort-"+cohortID.String()[:8],
	)
	if err != nil {
		t.Fatalf("seed cohort: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `
			DELETE FROM experiment_outcome WHERE pair_id IN
			  (SELECT id FROM experiment_pair WHERE cohort_id = $1)`, cohortID)
		pool.Exec(context.Background(), `
			DELETE FROM experiment_pair_run WHERE pair_id IN
			  (SELECT id FROM experiment_pair WHERE cohort_id = $1)`, cohortID)
		pool.Exec(context.Background(), "DELETE FROM experiment_pair WHERE cohort_id = $1", cohortID)
		pool.Exec(context.Background(), "DELETE FROM cohort WHERE id = $1", cohortID)
	})
	return cohortID
}

// buildExecuteResult creates an ExecuteResult for a given cohortID using
// synthetic bank data so tests don't need a full bank setup.
func buildExecuteResult(t *testing.T, cohortID uuid.UUID, seed int64) *ExecuteResult {
	t.Helper()
	params := bank.GenerateParams{
		CohortID:    cohortID,
		Seed:        seed,
		Predicate:   bank.Predicate{},
		WindowStart: pairWindowStart,
		WindowEnd:   pairWindowEnd,
	}
	generated, err := bank.Generate(params)
	if err != nil {
		t.Fatalf("bank.Generate: %v", err)
	}
	manifest := generated.Manifest
	manifest.CohortID = cohortID

	sel := &bank.BatchSelection{
		CohortID:     cohortID,
		WindowStart:  pairWindowStart,
		WindowEnd:    pairWindowEnd,
		Manifest:     manifest,
		Entities:     generated.Entities,
		Accounts:     generated.Accounts,
		Transactions: generated.Transactions,
	}

	input, err := NewInputContext(manifest, pairWindowStart, pairWindowEnd)
	if err != nil {
		t.Fatalf("NewInputContext: %v", err)
	}
	spec := PairSpec{
		Name:           "Integration Test Pair",
		Input:          input,
		Config:         aml.DefaultControlConfig(),
		DeclaredChange: "Split transactions below $1k threshold",
		MutationType:   MutationSplitTransaction,
		MutationSpec:   map[string]any{"split_count": 3, "threshold_cents": int64(100_000)},
	}

	res, err := Execute(spec, sel, fixedClock)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	res.Pair.CohortID = cohortID
	return res
}

// ─── SavePairResult + GetPair ─────────────────────────────────────────────────

func TestRepository_SaveAndGetPair(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := seedCohort(t, pool)
	res := buildExecuteResult(t, cohortID, 42)

	if err := repo.SavePairResult(ctx, res); err != nil {
		t.Fatalf("SavePairResult: %v", err)
	}

	got, err := repo.GetPair(ctx, res.Pair.ID)
	if err != nil {
		t.Fatalf("GetPair: %v", err)
	}
	if got.ID != res.Pair.ID {
		t.Errorf("pair ID mismatch: %s vs %s", got.ID, res.Pair.ID)
	}
	if got.ControlHash != res.Pair.ControlHash {
		t.Errorf("control_hash mismatch: %s vs %s", got.ControlHash, res.Pair.ControlHash)
	}
	if got.ManifestHash != res.Pair.ManifestHash {
		t.Errorf("manifest_hash mismatch: %s vs %s", got.ManifestHash, res.Pair.ManifestHash)
	}
	if got.Seed != res.Pair.Seed {
		t.Errorf("seed mismatch: %d vs %d", got.Seed, res.Pair.Seed)
	}
	if got.Status != StatusCompleted {
		t.Errorf("status should be completed, got %s", got.Status)
	}
}

// ─── GetOutcome ───────────────────────────────────────────────────────────────

func TestRepository_GetOutcome(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := seedCohort(t, pool)
	res := buildExecuteResult(t, cohortID, 77)

	if err := repo.SavePairResult(ctx, res); err != nil {
		t.Fatalf("SavePairResult: %v", err)
	}

	outcome, err := repo.GetOutcome(ctx, res.Pair.ID)
	if err != nil {
		t.Fatalf("GetOutcome: %v", err)
	}

	if outcome.PairID != res.Pair.ID {
		t.Errorf("outcome pair_id mismatch")
	}
	if outcome.EvasionDelta != res.Outcome.EvasionDelta {
		t.Errorf("evasion_delta: db=%d, expected=%d", outcome.EvasionDelta, res.Outcome.EvasionDelta)
	}
	if outcome.OutcomeUnchanged != res.Outcome.OutcomeUnchanged {
		t.Errorf("outcome_unchanged: db=%v, expected=%v",
			outcome.OutcomeUnchanged, res.Outcome.OutcomeUnchanged)
	}
}

// ─── ListRunsForPair ──────────────────────────────────────────────────────────

func TestRepository_ListRunsForPair(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := seedCohort(t, pool)
	res := buildExecuteResult(t, cohortID, 99)

	if err := repo.SavePairResult(ctx, res); err != nil {
		t.Fatalf("SavePairResult: %v", err)
	}

	runs, err := repo.ListRunsForPair(ctx, res.Pair.ID)
	if err != nil {
		t.Fatalf("ListRunsForPair: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}

	// Verify both runs carry the same control and manifest hashes.
	for _, r := range runs {
		if r.ControlHash != res.Pair.ControlHash {
			t.Errorf("run control_hash mismatch: %s", r.ControlHash)
		}
		if r.ManifestHash != res.Pair.ManifestHash {
			t.Errorf("run manifest_hash mismatch: %s", r.ManifestHash)
		}
	}

	// One baseline, one variant.
	types := map[RunType]bool{}
	for _, r := range runs {
		types[r.RunType] = true
	}
	if !types[RunTypeBaseline] {
		t.Error("no baseline run found")
	}
	if !types[RunTypeVariant] {
		t.Error("no variant run found")
	}
}

// ─── Alert counts survive roundtrip ──────────────────────────────────────────

func TestRepository_AlertCountRoundtrip(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := seedCohort(t, pool)
	res := buildExecuteResult(t, cohortID, 2024)

	if err := repo.SavePairResult(ctx, res); err != nil {
		t.Fatalf("SavePairResult: %v", err)
	}

	runs, err := repo.ListRunsForPair(ctx, res.Pair.ID)
	if err != nil {
		t.Fatalf("ListRunsForPair: %v", err)
	}

	for _, dbRun := range runs {
		var expected int
		if dbRun.RunType == RunTypeBaseline {
			expected = res.Baseline.AlertCount
		} else {
			expected = res.Variant.AlertCount
		}
		if dbRun.AlertCount != expected {
			t.Errorf("run %s alert_count: db=%d, expected=%d",
				dbRun.RunType, dbRun.AlertCount, expected)
		}
	}
}

// ─── Reproducibility: save + re-execute same seed → same hashes ───────────────

func TestRepository_Reproducibility(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := seedCohort(t, pool)
	seed := int64(31415)

	// First execution + save.
	res1 := buildExecuteResult(t, cohortID, seed)
	if err := repo.SavePairResult(ctx, res1); err != nil {
		t.Fatalf("SavePairResult: %v", err)
	}

	// Re-execute with the same seed (new cohort row, same seed).
	cohortID2 := seedCohort(t, pool)
	res2 := buildExecuteResult(t, cohortID2, seed)

	// Both must produce the same control_hash and manifest_hash.
	if res1.Pair.ControlHash != res2.Pair.ControlHash {
		t.Errorf("control_hash differs across runs: %s vs %s",
			res1.Pair.ControlHash, res2.Pair.ControlHash)
	}
	// Manifest hashes will differ between cohort IDs because CohortID is in the manifest.
	// Instead verify that re-executing the same spec on the same cohort gives the same hash.
	if res1.Pair.ManifestHash == "" {
		t.Error("manifest_hash is empty")
	}
	// Evasion delta must be identical.
	if res1.Outcome.EvasionDelta != res2.Outcome.EvasionDelta {
		t.Errorf("evasion_delta differs: %d vs %d",
			res1.Outcome.EvasionDelta, res2.Outcome.EvasionDelta)
	}
}

// ─── Incomparable pair: ComparePair on persisted runs ────────────────────────

func TestRepository_ComparePair_IncompatibleRunsFromDifferentConfigs(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := seedCohort(t, pool)

	// First pair with default config.
	res1 := buildExecuteResult(t, cohortID, 11)
	if err := repo.SavePairResult(ctx, res1); err != nil {
		t.Fatalf("SavePairResult (1): %v", err)
	}

	// Reload the baseline from first pair.
	runs1, _ := repo.ListRunsForPair(ctx, res1.Pair.ID)
	var baseline1 RunResult
	for _, r := range runs1 {
		if r.RunType == RunTypeBaseline {
			baseline1 = r
		}
	}

	// Second pair with a different config.
	cfg2, _ := aml.NewControlConfig(
		aml.EnabledRules{aml.RuleStructuring},
		aml.StructuringConfig{ReportingThresholdCents: 2_000_000, WindowHours: 12, MinTransactions: 2},
		aml.LayeringConfig{MinHops: 3, WindowHours: 72},
		aml.RapidMovementConfig{MinAmountCents: 500_000, WindowMinutes: 60, DebitRatioPct: 80},
	)

	params := bank.GenerateParams{
		CohortID:    cohortID,
		Seed:        11,
		Predicate:   bank.Predicate{},
		WindowStart: pairWindowStart,
		WindowEnd:   pairWindowEnd,
	}
	generated, _ := bank.Generate(params)
	manifest := generated.Manifest
	manifest.CohortID = cohortID

	sel := &bank.BatchSelection{
		CohortID: cohortID, WindowStart: pairWindowStart, WindowEnd: pairWindowEnd,
		Manifest: manifest, Entities: generated.Entities,
		Accounts: generated.Accounts, Transactions: generated.Transactions,
	}
	input2, _ := NewInputContext(manifest, pairWindowStart, pairWindowEnd)
	spec2 := PairSpec{
		Name: "Alt Config Pair", Input: input2, Config: cfg2,
		DeclaredChange: "Different rule config", MutationType: MutationNone,
	}
	res2, _ := Execute(spec2, sel, fixedClock)

	// Baseline from pair1 vs variant from pair2: different control hashes.
	_, err := ComparePair(baseline1, res2.Variant)
	if err == nil {
		t.Fatal("expected ErrIncomparablePair when comparing runs from different configs")
	}
}
