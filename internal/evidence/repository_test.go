// Package evidence — repository_test.go
//
// Integration test for the evidence bundle repository. Requires a migrated
// PostgreSQL database; skipped in -short mode and when no DB is reachable.
package evidence

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/coverage"
)

func testDBURL() string {
	if url := os.Getenv("DATABASE_TEST_URL"); url != "" {
		return url
	}
	return "postgres://localhost/fincrime_test?sslmode=disable"
}

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDBURL())
	if err != nil {
		t.Skipf("skipping: cannot create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping: database not reachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestSaveAndGetBundleRoundTrip(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()

	// A bundle references an experiment_pair (FK), which references a cohort.
	cohort, err := bank.NewRepository(pool).CreateCohort(ctx, bank.Cohort{
		ID:               uuid.New(),
		Name:             "evidence-test-cohort",
		Seed:             1,
		Predicate:        bank.Predicate{},
		GeneratorVersion: "test",
	})
	if err != nil {
		t.Fatalf("create cohort: %v", err)
	}

	pairID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO experiment_pair
		  (id, name, cohort_id, control_hash, manifest_hash, seed,
		   declared_change, mutation_type, mutation_spec, status)
		VALUES ($1,'p',$2,'ch','mh',1,'d','split_transaction','{}'::jsonb,'completed')`,
		pairID, cohort.ID); err != nil {
		t.Fatalf("insert pair: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM evidence_bundle WHERE experiment_id = $1`, pairID)
		pool.Exec(ctx, `DELETE FROM experiment_pair WHERE id = $1`, pairID)
		pool.Exec(ctx, `DELETE FROM cohort WHERE id = $1`, cohort.ID)
	})

	repo := NewRepository(pool)

	b := Bundle{
		RunID:          pairID,
		AMLRuleVersion: "ch",
		CohortSnapshot: CohortSnapshot{CohortID: cohort.ID, Seed: 1},
		CoverageDelta:  []coverage.RuleCoverage{},
		CreatedAt:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := repo.Save(ctx, b); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.Get(ctx, pairID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RunID != pairID {
		t.Errorf("RunID = %s, want %s", got.RunID, pairID)
	}
	if got.Checksum == "" {
		t.Error("persisted bundle has empty checksum")
	}
	if ok, err := VerifyChecksum(got); err != nil || !ok {
		t.Errorf("round-tripped bundle checksum invalid: ok=%v err=%v", ok, err)
	}

	// Save is an idempotent upsert (one bundle per experiment).
	if err := repo.Save(ctx, got); err != nil {
		t.Fatalf("re-Save: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM evidence_bundle WHERE experiment_id = $1`, pairID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("expected exactly 1 bundle row, got %d", n)
	}
}

func TestGetMissingBundle(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)

	if _, err := repo.Get(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected error for missing bundle")
	}
}
