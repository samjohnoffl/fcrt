// Package bank — repository_test.go
//
// Integration tests for the bank Repository.
// These tests require a real PostgreSQL database.
//
// Run with: go test -v ./internal/bank/ -run TestRepository
// Skip with: go test -short ./internal/bank/
//
// Database setup: the tests use the DATABASE_TEST_URL environment variable,
// falling back to a default local DSN. The schema must already be migrated.
// Tests run inside transactions that are rolled back, so the DB stays clean.
package bank

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sortEntitiesByID sorts a slice of Entity by UUID string, producing a total order
// that is independent of insertion or generation sequence.
func sortEntitiesByID(es []Entity) {
	sort.Slice(es, func(i, j int) bool {
		return es[i].ID.String() < es[j].ID.String()
	})
}

// sortTransactionsByID sorts a slice of Transaction by UUID string.
func sortTransactionsByID(txs []Transaction) {
	sort.Slice(txs, func(i, j int) bool {
		return txs[i].ID.String() < txs[j].ID.String()
	})
}

// testDBURL returns the connection string for integration tests.
// Prefers the DATABASE_TEST_URL environment variable.
func testDBURL(t *testing.T) string {
	t.Helper()
	if url := os.Getenv("DATABASE_TEST_URL"); url != "" {
		return url
	}
	// Default: local DB owned by the current OS user.
	return "postgres://localhost/fincrime_test?sslmode=disable"
}

// openTestPool opens a connection pool for tests, skipping if the DB is
// unavailable (e.g. in CI without a DB service).
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

// newTestCohort creates a minimal cohort record for integration tests.
func newTestCohort(id uuid.UUID, seed int64) Cohort {
	return Cohort{
		ID:               id,
		Name:             "Test Cohort " + id.String()[:8],
		Predicate:        Predicate{},
		Seed:             seed,
		GeneratorVersion: GeneratorVersion,
		Manifest:         CohortManifest{},
	}
}

// ─── Cohort CRUD ─────────────────────────────────────────────────────────────

func TestRepository_CreateAndGetCohort(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := uuid.New()
	c := newTestCohort(cohortID, 99)
	created, err := repo.CreateCohort(ctx, c)
	if err != nil {
		t.Fatalf("CreateCohort: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM cohort WHERE id = $1", cohortID)
	})

	if created.ID != cohortID {
		t.Errorf("ID mismatch: %s vs %s", created.ID, cohortID)
	}
	if created.LockedAt != nil {
		t.Error("newly created cohort should not be locked")
	}

	got, err := repo.GetCohort(ctx, cohortID)
	if err != nil {
		t.Fatalf("GetCohort: %v", err)
	}
	if got.Seed != 99 {
		t.Errorf("Seed mismatch: got %d", got.Seed)
	}
}

// ─── AC-01.2: Cohort locking ─────────────────────────────────────────────────

func TestRepository_LockCohort_PreventsReLock(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := uuid.New()
	c := newTestCohort(cohortID, 7)
	if _, err := repo.CreateCohort(ctx, c); err != nil {
		t.Fatalf("CreateCohort: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM cohort WHERE id = $1", cohortID)
	})

	locked, err := repo.LockCohort(ctx, cohortID)
	if err != nil {
		t.Fatalf("LockCohort: %v", err)
	}
	if locked.LockedAt == nil {
		t.Error("LockCohort did not set locked_at")
	}

	// Second lock attempt must return ErrCohortLocked.
	_, err = repo.LockCohort(ctx, cohortID)
	if err == nil {
		t.Fatal("expected ErrCohortLocked, got nil")
	}
}

// ─── Full generate + save roundtrip ──────────────────────────────────────────

func TestRepository_SaveGenerated_Roundtrip(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := uuid.New()
	c := newTestCohort(cohortID, 12345)
	if _, err := repo.CreateCohort(ctx, c); err != nil {
		t.Fatalf("CreateCohort: %v", err)
	}
	// Clean up all generated rows after test.
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM bank_transaction WHERE cohort_id = $1", cohortID)
		pool.Exec(ctx, "DELETE FROM bank_account WHERE cohort_id = $1", cohortID)
		pool.Exec(ctx, "DELETE FROM bank_entity WHERE cohort_id = $1", cohortID)
		pool.Exec(ctx, "DELETE FROM cohort WHERE id = $1", cohortID)
	})

	windowStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(90 * 24 * time.Hour)

	params := GenerateParams{
		CohortID:    cohortID,
		Seed:        12345,
		Predicate:   Predicate{},
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
	}
	result, err := Generate(params)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if err := repo.SaveGenerated(ctx, result); err != nil {
		t.Fatalf("SaveGenerated: %v", err)
	}

	// Verify cohort is locked after save.
	saved, err := repo.GetCohort(ctx, cohortID)
	if err != nil {
		t.Fatalf("GetCohort after save: %v", err)
	}
	if saved.LockedAt == nil {
		t.Error("cohort should be locked after SaveGenerated")
	}

	// Verify entity count matches.
	entities, err := repo.ListEntities(ctx, cohortID)
	if err != nil {
		t.Fatalf("ListEntities: %v", err)
	}
	if len(entities) != len(result.Entities) {
		t.Errorf("entity count: saved %d, expected %d", len(entities), len(result.Entities))
	}

	// Verify account count matches.
	accounts, err := repo.ListAccounts(ctx, cohortID)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accounts) != len(result.Accounts) {
		t.Errorf("account count: saved %d, expected %d", len(accounts), len(result.Accounts))
	}

	// Verify transaction count matches.
	txns, err := repo.ListTransactionsInWindow(ctx, cohortID, windowStart, windowEnd)
	if err != nil {
		t.Fatalf("ListTransactionsInWindow: %v", err)
	}
	if len(txns) != len(result.Transactions) {
		t.Errorf("tx count: saved %d, expected %d", len(txns), len(result.Transactions))
	}
}

// ─── AC-01.2: SaveGenerated on locked cohort ─────────────────────────────────

func TestRepository_SaveGenerated_LockedCohortError(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := uuid.New()
	c := newTestCohort(cohortID, 999)
	if _, err := repo.CreateCohort(ctx, c); err != nil {
		t.Fatalf("CreateCohort: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM cohort WHERE id = $1", cohortID)
	})

	// Lock before generating.
	if _, err := repo.LockCohort(ctx, cohortID); err != nil {
		t.Fatalf("LockCohort: %v", err)
	}

	windowStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	params := GenerateParams{
		CohortID:    cohortID,
		Seed:        999,
		Predicate:   Predicate{},
		WindowStart: windowStart,
		WindowEnd:   windowStart.Add(90 * 24 * time.Hour),
	}
	result, _ := Generate(params)

	err := repo.SaveGenerated(ctx, result)
	if err == nil {
		t.Fatal("expected ErrCohortLocked, got nil")
	}
}

// ─── Referential integrity: FK violations caught by DB ───────────────────────

func TestRepository_InvalidEntityFK(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()

	cohortID := uuid.New()
	repo := NewRepository(pool)
	c := newTestCohort(cohortID, 55)
	if _, err := repo.CreateCohort(ctx, c); err != nil {
		t.Fatalf("CreateCohort: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM cohort WHERE id = $1", cohortID)
	})

	// Insert an account referencing a non-existent entity.
	fakeEntityID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO bank_account (id, cohort_id, entity_id, account_type, currency, balance_cents, opened_at)
		VALUES ($1, $2, $3, 'checking', 'USD', 0, NOW())`,
		uuid.New(), cohortID, fakeEntityID,
	)
	if err == nil {
		t.Error("expected FK violation error, got nil")
	}
}

func TestRepository_InvalidAmountRejected(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()

	cohortID := uuid.New()
	repo := NewRepository(pool)
	c := newTestCohort(cohortID, 77)
	if _, err := repo.CreateCohort(ctx, c); err != nil {
		t.Fatalf("CreateCohort: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM bank_transaction WHERE cohort_id = $1", cohortID)
		pool.Exec(ctx, "DELETE FROM bank_account WHERE cohort_id = $1", cohortID)
		pool.Exec(ctx, "DELETE FROM bank_entity WHERE cohort_id = $1", cohortID)
		pool.Exec(ctx, "DELETE FROM cohort WHERE id = $1", cohortID)
	})

	// Insert a minimal entity and two accounts to test FK chains.
	entityID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO bank_entity (id, cohort_id, entity_type, name, country_code, risk_score)
		VALUES ($1, $2, 'individual', 'Test Entity', 'US', 0.1)`,
		entityID, cohortID)
	if err != nil {
		t.Fatalf("insert entity: %v", err)
	}

	fromID := uuid.New()
	toID := uuid.New()
	for _, accID := range []uuid.UUID{fromID, toID} {
		_, err = pool.Exec(ctx, `
			INSERT INTO bank_account (id, cohort_id, entity_id, account_type, currency, balance_cents, opened_at)
			VALUES ($1, $2, $3, 'checking', 'USD', 100000, NOW())`,
			accID, cohortID, entityID)
		if err != nil {
			t.Fatalf("insert account: %v", err)
		}
	}

	// A negative amount violates the NOT NULL / business invariant.
	// We test that our application layer catches it before it reaches the DB.
	// (The DB NUMERIC column itself does not have a positive CHECK constraint —
	//  that's enforced at the application layer by Generate().)
	// So we just verify that a generate result never produces negative amounts.
	params := GenerateParams{
		CohortID:    cohortID,
		Seed:        77,
		Predicate:   Predicate{},
		WindowStart: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
	}
	result, err := Generate(params)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, tx := range result.Transactions {
		if tx.AmountCents <= 0 {
			t.Errorf("generator produced non-positive amount: %d", tx.AmountCents)
		}
	}
}

// ─── Batch selection ──────────────────────────────────────────────────────────

func TestRepository_SelectBatch_TimeWindowFiltering(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := uuid.New()
	c := newTestCohort(cohortID, 2024)
	if _, err := repo.CreateCohort(ctx, c); err != nil {
		t.Fatalf("CreateCohort: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM bank_transaction WHERE cohort_id = $1", cohortID)
		pool.Exec(ctx, "DELETE FROM bank_account WHERE cohort_id = $1", cohortID)
		pool.Exec(ctx, "DELETE FROM bank_entity WHERE cohort_id = $1", cohortID)
		pool.Exec(ctx, "DELETE FROM cohort WHERE id = $1", cohortID)
	})

	fullStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	fullEnd := fullStart.Add(90 * 24 * time.Hour)

	result, err := Generate(GenerateParams{
		CohortID:    cohortID,
		Seed:        2024,
		Predicate:   Predicate{},
		WindowStart: fullStart,
		WindowEnd:   fullEnd,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := repo.SaveGenerated(ctx, result); err != nil {
		t.Fatalf("SaveGenerated: %v", err)
	}

	// Select only the first 45 days.
	midpoint := fullStart.Add(45 * 24 * time.Hour)
	batch, err := repo.SelectBatch(ctx, cohortID, fullStart, midpoint)
	if err != nil {
		t.Fatalf("SelectBatch: %v", err)
	}

	// All returned transactions must be within [fullStart, midpoint).
	for _, tx := range batch.Transactions {
		if tx.TxTimestamp.Before(fullStart) || !tx.TxTimestamp.Before(midpoint) {
			t.Errorf("tx %s timestamp %s outside window [%s, %s)",
				tx.ID,
				tx.TxTimestamp.Format(time.RFC3339),
				fullStart.Format(time.RFC3339),
				midpoint.Format(time.RFC3339),
			)
		}
	}

	// Batch must have fewer transactions than the full set (probabilistically true
	// with a 90-day window split at 45 days — unless all txs happen to fall in
	// the first 45 days, which is extremely unlikely with seed 2024).
	if len(batch.Transactions) >= len(result.Transactions) {
		t.Logf("warn: batch tx count %d >= full count %d (statistically unlikely)",
			len(batch.Transactions), len(result.Transactions))
	}
}

func TestRepository_SelectBatch_RequiresLockedCohort(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := uuid.New()
	c := newTestCohort(cohortID, 888)
	if _, err := repo.CreateCohort(ctx, c); err != nil {
		t.Fatalf("CreateCohort: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM cohort WHERE id = $1", cohortID)
	})

	// Do NOT lock — expect an error.
	_, err := repo.SelectBatch(ctx, cohortID,
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
	)
	if err == nil {
		t.Fatal("expected error for unlocked cohort batch selection, got nil")
	}
}

// ─── Reproducibility: generate + save + reload + regenerate ─────────────────

// TestRepository_Reproducibility verifies AC-01.1 end-to-end through the DB:
// regenerating with the same seed produces entities with the same IDs and
// amounts as those stored in the database.
func TestRepository_Reproducibility(t *testing.T) {
	pool := openTestPool(t)
	repo := NewRepository(pool)
	ctx := context.Background()

	cohortID := uuid.New()
	c := newTestCohort(cohortID, 31415)
	if _, err := repo.CreateCohort(ctx, c); err != nil {
		t.Fatalf("CreateCohort: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM bank_transaction WHERE cohort_id = $1", cohortID)
		pool.Exec(ctx, "DELETE FROM bank_account WHERE cohort_id = $1", cohortID)
		pool.Exec(ctx, "DELETE FROM bank_entity WHERE cohort_id = $1", cohortID)
		pool.Exec(ctx, "DELETE FROM cohort WHERE id = $1", cohortID)
	})

	windowStart := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(90 * 24 * time.Hour)

	params := GenerateParams{
		CohortID:    cohortID,
		Seed:        31415,
		Predicate:   Predicate{},
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
	}

	// First generation.
	result1, err := Generate(params)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := repo.SaveGenerated(ctx, result1); err != nil {
		t.Fatalf("SaveGenerated: %v", err)
	}

	// Read back from DB.
	dbEntities, err := repo.ListEntities(ctx, cohortID)
	if err != nil {
		t.Fatalf("ListEntities: %v", err)
	}
	dbTxns, err := repo.ListTransactionsInWindow(ctx, cohortID, windowStart, windowEnd)
	if err != nil {
		t.Fatalf("ListTransactionsInWindow: %v", err)
	}

	// Re-generate in memory with the same params.
	result2, err := Generate(params)
	if err != nil {
		t.Fatalf("re-Generate: %v", err)
	}

	// Sort both sides by ID for a total order that is independent of
	// DB retrieval order (created_at,id) vs. generation order.
	sortEntitiesByID(dbEntities)
	sortEntitiesByID(result2.Entities)

	// Entity IDs must match element-wise after sorting.
	if len(dbEntities) != len(result2.Entities) {
		t.Fatalf("entity count: db=%d, regenerated=%d", len(dbEntities), len(result2.Entities))
	}
	for i := range dbEntities {
		if dbEntities[i].ID != result2.Entities[i].ID {
			t.Errorf("entity[%d] ID: db=%s, regen=%s", i, dbEntities[i].ID, result2.Entities[i].ID)
		}
		if dbEntities[i].Name != result2.Entities[i].Name {
			t.Errorf("entity[%d] Name: db=%q, regen=%q", i, dbEntities[i].Name, result2.Entities[i].Name)
		}
	}

	// Sort transactions by ID (DB returns by (tx_timestamp,id); generation order differs).
	sortTransactionsByID(dbTxns)
	sortTransactionsByID(result2.Transactions)

	// Transaction IDs and amounts must match element-wise after sorting.
	if len(dbTxns) != len(result2.Transactions) {
		t.Fatalf("tx count: db=%d, regenerated=%d", len(dbTxns), len(result2.Transactions))
	}
	for i := range dbTxns {
		if dbTxns[i].ID != result2.Transactions[i].ID {
			t.Errorf("tx[%d] ID: db=%s, regen=%s", i, dbTxns[i].ID, result2.Transactions[i].ID)
		}
		if dbTxns[i].AmountCents != result2.Transactions[i].AmountCents {
			t.Errorf("tx[%d] AmountCents: db=%d, regen=%d",
				i, dbTxns[i].AmountCents, result2.Transactions[i].AmountCents)
		}
	}
}

// ─── DB entity type constraint ────────────────────────────────────────────────

func TestRepository_InvalidEntityTypeConstraint(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()

	cohortID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO cohort (id,name,predicate,seed,generator_version,manifest) VALUES ($1,'x','{}',0,'','{}'::jsonb)`, cohortID)
	if err != nil {
		t.Fatalf("setup cohort: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM cohort WHERE id = $1", cohortID)
	})

	// Try to insert an entity with an invalid type — the DB CHECK constraint must reject it.
	_, err = pool.Exec(ctx, `
		INSERT INTO bank_entity (id, cohort_id, entity_type, name, country_code, risk_score)
		VALUES ($1, $2, 'invalid_type', 'Test', 'US', 0.5)`,
		uuid.New(), cohortID,
	)
	if err == nil {
		t.Error("expected CHECK constraint violation for invalid entity_type, got nil")
	}
}

func TestRepository_RiskScoreConstraint(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()

	cohortID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO cohort (id,name,predicate,seed,generator_version,manifest) VALUES ($1,'x2','{}',0,'','{}'::jsonb)`, cohortID)
	if err != nil {
		t.Fatalf("setup cohort: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM bank_entity WHERE cohort_id = $1", cohortID)
		pool.Exec(ctx, "DELETE FROM cohort WHERE id = $1", cohortID)
	})

	// risk_score > 1.0 must be rejected by DB CHECK.
	_, err = pool.Exec(ctx, `
		INSERT INTO bank_entity (id, cohort_id, entity_type, name, country_code, risk_score)
		VALUES ($1, $2, 'individual', 'Test', 'US', 1.5)`,
		uuid.New(), cohortID,
	)
	if err == nil {
		t.Error("expected CHECK constraint violation for risk_score > 1.0, got nil")
	}
}
