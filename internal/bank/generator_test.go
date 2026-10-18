// Package bank — generator_test.go
//
// Tests for the deterministic bank generator.
// All tests in this file run without a database connection.
package bank

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// ─── Test helpers ─────────────────────────────────────────────────────────────

var (
	testWindowStart = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testWindowEnd   = testWindowStart.Add(90 * 24 * time.Hour)
	testCohortID    = uuid.MustParse("11111111-1111-4111-8111-111111111111")
)

func defaultParams() GenerateParams {
	return GenerateParams{
		CohortID:    testCohortID,
		Seed:        42,
		Predicate:   Predicate{},
		WindowStart: testWindowStart,
		WindowEnd:   testWindowEnd,
	}
}

// ─── AC-01.1: Deterministic generation ───────────────────────────────────────

func TestGenerate_Deterministic(t *testing.T) {
	p := defaultParams()

	r1, err := Generate(p)
	if err != nil {
		t.Fatalf("first Generate failed: %v", err)
	}
	r2, err := Generate(p)
	if err != nil {
		t.Fatalf("second Generate failed: %v", err)
	}

	// Entity count must be identical.
	if len(r1.Entities) != len(r2.Entities) {
		t.Errorf("entity count mismatch: %d vs %d", len(r1.Entities), len(r2.Entities))
	}
	// Account count must be identical.
	if len(r1.Accounts) != len(r2.Accounts) {
		t.Errorf("account count mismatch: %d vs %d", len(r1.Accounts), len(r2.Accounts))
	}
	// Transaction count must be identical.
	if len(r1.Transactions) != len(r2.Transactions) {
		t.Errorf("tx count mismatch: %d vs %d", len(r1.Transactions), len(r2.Transactions))
	}

	// Each entity must have byte-identical fields.
	for i := range r1.Entities {
		e1, e2 := r1.Entities[i], r2.Entities[i]
		if e1.ID != e2.ID {
			t.Errorf("entity[%d].ID mismatch: %s vs %s", i, e1.ID, e2.ID)
		}
		if e1.Name != e2.Name {
			t.Errorf("entity[%d].Name mismatch: %q vs %q", i, e1.Name, e2.Name)
		}
		if e1.EntityType != e2.EntityType {
			t.Errorf("entity[%d].EntityType mismatch", i)
		}
		if e1.CountryCode != e2.CountryCode {
			t.Errorf("entity[%d].CountryCode mismatch", i)
		}
		if e1.RiskScore != e2.RiskScore {
			t.Errorf("entity[%d].RiskScore mismatch: %f vs %f", i, e1.RiskScore, e2.RiskScore)
		}
	}

	// Each transaction must have byte-identical key fields.
	for i := range r1.Transactions {
		t1, t2 := r1.Transactions[i], r2.Transactions[i]
		if t1.ID != t2.ID {
			t.Errorf("tx[%d].ID mismatch: %s vs %s", i, t1.ID, t2.ID)
		}
		if t1.AmountCents != t2.AmountCents {
			t.Errorf("tx[%d].AmountCents mismatch: %d vs %d", i, t1.AmountCents, t2.AmountCents)
		}
		if !t1.TxTimestamp.Equal(t2.TxTimestamp) {
			t.Errorf("tx[%d].TxTimestamp mismatch", i)
		}
	}
}

// Different seeds must produce different data.
func TestGenerate_DifferentSeedsDifferentData(t *testing.T) {
	p1 := defaultParams()
	p1.Seed = 1

	p2 := defaultParams()
	p2.Seed = 2

	r1, _ := Generate(p1)
	r2, _ := Generate(p2)

	// Entity UUIDs must differ (guaranteed by seed-derived namespace).
	if len(r1.Entities) > 0 && len(r2.Entities) > 0 {
		if r1.Entities[0].ID == r2.Entities[0].ID {
			t.Error("different seeds produced the same first entity UUID")
		}
	}
}

// ─── AC-01.3: Predicate filtering ────────────────────────────────────────────

func TestGenerate_PredicateEntityType(t *testing.T) {
	et := EntityTypeIndividual
	p := defaultParams()
	p.Predicate = Predicate{EntityType: &et}

	result, err := Generate(p)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	for _, e := range result.Entities {
		if e.EntityType != EntityTypeIndividual {
			t.Errorf("entity %s has type %q, want individual", e.ID, e.EntityType)
		}
	}
}

func TestGenerate_PredicateCountryCode(t *testing.T) {
	cc := "US"
	p := defaultParams()
	p.Predicate = Predicate{CountryCode: &cc}

	result, err := Generate(p)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	for _, e := range result.Entities {
		if e.CountryCode != "US" {
			t.Errorf("entity %s has country %q, want US", e.ID, e.CountryCode)
		}
	}
}

func TestGenerate_PredicateBothFilters(t *testing.T) {
	et := EntityTypeShell
	cc := "KY"
	p := defaultParams()
	p.Predicate = Predicate{EntityType: &et, CountryCode: &cc}

	result, err := Generate(p)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	for _, e := range result.Entities {
		if e.EntityType != EntityTypeShell {
			t.Errorf("entity has type %q, want shell", e.EntityType)
		}
		if e.CountryCode != "KY" {
			t.Errorf("entity has country %q, want KY", e.CountryCode)
		}
	}
}

// ─── Ledger invariants ────────────────────────────────────────────────────────

func TestGenerate_AllAmountsPositive(t *testing.T) {
	result, err := Generate(defaultParams())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	for _, tx := range result.Transactions {
		if tx.AmountCents <= 0 {
			t.Errorf("tx %s has non-positive amount: %d cents", tx.ID, tx.AmountCents)
		}
	}
}

func TestGenerate_AllAccountBalancesNonNegative(t *testing.T) {
	result, err := Generate(defaultParams())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	// Note: final balances may be 0 (exactly drained) but never negative.
	for _, a := range result.Accounts {
		if a.BalanceCents < 0 {
			t.Errorf("account %s has negative final balance: %d cents", a.ID, a.BalanceCents)
		}
	}
}

func TestGenerate_ValidAccountReferences(t *testing.T) {
	result, err := Generate(defaultParams())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	accountIDs := make(map[uuid.UUID]bool, len(result.Accounts))
	for _, a := range result.Accounts {
		accountIDs[a.ID] = true
	}

	for _, tx := range result.Transactions {
		if !accountIDs[tx.FromAccountID] {
			t.Errorf("tx %s: FromAccountID %s not in accounts", tx.ID, tx.FromAccountID)
		}
		if !accountIDs[tx.ToAccountID] {
			t.Errorf("tx %s: ToAccountID %s not in accounts", tx.ID, tx.ToAccountID)
		}
		if tx.FromAccountID == tx.ToAccountID {
			t.Errorf("tx %s: self-transfer (from == to)", tx.ID)
		}
	}
}

func TestGenerate_ValidCohortReferences(t *testing.T) {
	p := defaultParams()
	result, err := Generate(p)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	for _, e := range result.Entities {
		if e.CohortID != p.CohortID {
			t.Errorf("entity %s has wrong cohort_id", e.ID)
		}
	}
	for _, a := range result.Accounts {
		if a.CohortID != p.CohortID {
			t.Errorf("account %s has wrong cohort_id", a.ID)
		}
	}
	for _, tx := range result.Transactions {
		if tx.CohortID != p.CohortID {
			t.Errorf("tx %s has wrong cohort_id", tx.ID)
		}
	}
}

// ─── Timestamp validity ───────────────────────────────────────────────────────

func TestGenerate_TransactionsWithinWindow(t *testing.T) {
	p := defaultParams()
	result, err := Generate(p)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	for _, tx := range result.Transactions {
		if tx.TxTimestamp.Before(p.WindowStart) {
			t.Errorf("tx %s timestamp %s is before window start %s",
				tx.ID, tx.TxTimestamp.Format(time.RFC3339), p.WindowStart.Format(time.RFC3339))
		}
		if !tx.TxTimestamp.Before(p.WindowEnd) {
			t.Errorf("tx %s timestamp %s is not before window end %s",
				tx.ID, tx.TxTimestamp.Format(time.RFC3339), p.WindowEnd.Format(time.RFC3339))
		}
	}
}

func TestGenerate_TransactionsSortedByTimestamp(t *testing.T) {
	result, err := Generate(defaultParams())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	txs := result.Transactions
	for i := 1; i < len(txs); i++ {
		prev, curr := txs[i-1], txs[i]
		if curr.TxTimestamp.Before(prev.TxTimestamp) {
			t.Errorf("transactions not sorted: tx[%d]=%s before tx[%d]=%s",
				i, curr.TxTimestamp, i-1, prev.TxTimestamp)
			break
		}
	}
}

// ─── Unique IDs ───────────────────────────────────────────────────────────────

func TestGenerate_UniqueEntityIDs(t *testing.T) {
	result, err := Generate(defaultParams())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	seen := make(map[uuid.UUID]bool)
	for _, e := range result.Entities {
		if seen[e.ID] {
			t.Errorf("duplicate entity ID: %s", e.ID)
		}
		seen[e.ID] = true
	}
}

func TestGenerate_UniqueTransactionIDs(t *testing.T) {
	result, err := Generate(defaultParams())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	seen := make(map[uuid.UUID]bool)
	for _, tx := range result.Transactions {
		if seen[tx.ID] {
			t.Errorf("duplicate tx ID: %s", tx.ID)
		}
		seen[tx.ID] = true
	}
}

// ─── Invalid params ───────────────────────────────────────────────────────────

func TestGenerate_RejectsNilCohortID(t *testing.T) {
	p := defaultParams()
	p.CohortID = uuid.Nil
	_, err := Generate(p)
	if err == nil {
		t.Fatal("expected error for nil CohortID, got nil")
	}
}

func TestGenerate_RejectsWindowEndBeforeStart(t *testing.T) {
	p := defaultParams()
	p.WindowEnd = p.WindowStart.Add(-1 * time.Hour)
	_, err := Generate(p)
	if err == nil {
		t.Fatal("expected error for WindowEnd before WindowStart, got nil")
	}
}

func TestGenerate_RejectsInvalidCountryCode(t *testing.T) {
	p := defaultParams()
	cc := "USA" // must be 2 chars
	p.Predicate = Predicate{CountryCode: &cc}
	_, err := Generate(p)
	if err == nil {
		t.Fatal("expected error for invalid country code, got nil")
	}
}

func TestGenerate_RejectsInvalidEntityType(t *testing.T) {
	p := defaultParams()
	bad := EntityType("unknown")
	p.Predicate = Predicate{EntityType: &bad}
	_, err := Generate(p)
	if err == nil {
		t.Fatal("expected error for invalid entity type, got nil")
	}
}

// ─── Manifest ─────────────────────────────────────────────────────────────────

func TestGenerate_ManifestCounts(t *testing.T) {
	result, err := Generate(defaultParams())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	m := result.Manifest
	if m.EntityCount != len(result.Entities) {
		t.Errorf("manifest.EntityCount=%d, actual=%d", m.EntityCount, len(result.Entities))
	}
	if m.AccountCount != len(result.Accounts) {
		t.Errorf("manifest.AccountCount=%d, actual=%d", m.AccountCount, len(result.Accounts))
	}
	if m.TxCount != len(result.Transactions) {
		t.Errorf("manifest.TxCount=%d, actual=%d", m.TxCount, len(result.Transactions))
	}
	if m.GeneratorVersion != GeneratorVersion {
		t.Errorf("manifest.GeneratorVersion=%q, want %q", m.GeneratorVersion, GeneratorVersion)
	}
	if m.Seed != 42 {
		t.Errorf("manifest.Seed=%d, want 42", m.Seed)
	}
}

func TestGenerate_ManifestWindowTimes(t *testing.T) {
	p := defaultParams()
	result, err := Generate(p)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if !result.Manifest.WindowStart.Equal(p.WindowStart) {
		t.Errorf("manifest WindowStart mismatch")
	}
	if !result.Manifest.WindowEnd.Equal(p.WindowEnd) {
		t.Errorf("manifest WindowEnd mismatch")
	}
}

// ─── Entity-Account referential integrity (in-memory) ───────────────────────

func TestGenerate_AllAccountsReferencedEntities(t *testing.T) {
	result, err := Generate(defaultParams())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	entityIDs := make(map[uuid.UUID]bool, len(result.Entities))
	for _, e := range result.Entities {
		entityIDs[e.ID] = true
	}

	for _, a := range result.Accounts {
		if !entityIDs[a.EntityID] {
			t.Errorf("account %s references unknown entity %s", a.ID, a.EntityID)
		}
	}
}

// ─── RiskScore bounds ────────────────────────────────────────────────────────

func TestGenerate_RiskScoreBounds(t *testing.T) {
	result, err := Generate(defaultParams())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	for _, e := range result.Entities {
		if e.RiskScore < 0.0 || e.RiskScore > 1.0 {
			t.Errorf("entity %s risk_score %f out of [0,1]", e.ID, e.RiskScore)
		}
	}
}

// ─── Monetary conversion ──────────────────────────────────────────────────────

func TestCentsToDecimalStr(t *testing.T) {
	cases := []struct {
		cents    int64
		expected string
	}{
		{0, "0.00"},
		{1, "0.01"},
		{100, "1.00"},
		{12345, "123.45"},
		{-100, "-1.00"},
		{999999, "9999.99"},
	}
	for _, c := range cases {
		got := centsToDecimalStr(c.cents)
		if got != c.expected {
			t.Errorf("centsToDecimalStr(%d) = %q, want %q", c.cents, got, c.expected)
		}
	}
}

func TestDecimalStrToCents(t *testing.T) {
	cases := []struct {
		str      string
		expected int64
	}{
		{"0.00", 0},
		{"0.01", 1},
		{"1.00", 100},
		{"123.45", 12345},
		{"-1.00", -100},
		{"9999.99", 999999},
		{"123", 12300},
		{"123.4", 12340},
		{"123.456", 12345}, // truncated
	}
	for _, c := range cases {
		got := decimalStrToCents(c.str)
		if got != c.expected {
			t.Errorf("decimalStrToCents(%q) = %d, want %d", c.str, got, c.expected)
		}
	}
}

func TestCentsRoundtrip(t *testing.T) {
	for _, cents := range []int64{0, 1, 100, 12345, 999999, 1_000_000} {
		s := centsToDecimalStr(cents)
		back := decimalStrToCents(s)
		if back != cents {
			t.Errorf("roundtrip(%d) = %d (via %q)", cents, back, s)
		}
	}
}

// ─── EntityCount bounds ───────────────────────────────────────────────────────

func TestGenerate_EntityCountRespectsBounds(t *testing.T) {
	min, max := 5, 8
	p := defaultParams()
	p.Predicate = Predicate{MinEntities: &min, MaxEntities: &max}

	// Run multiple seeds to check bounds hold consistently.
	for seed := int64(1); seed <= 20; seed++ {
		p.Seed = seed
		result, err := Generate(p)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		n := len(result.Entities)
		if n < min || n > max {
			t.Errorf("seed %d: entity count %d not in [%d,%d]", seed, n, min, max)
		}
	}
}

// ─── ErrCohortLocked sentinel ────────────────────────────────────────────────

func TestErrCohortLocked_IsError(t *testing.T) {
	var err error = ErrCohortLocked
	if err.Error() == "" {
		t.Error("ErrCohortLocked.Error() returned empty string")
	}
}

// ─── Cohort isolation: same seed, different cohorts must not collide ──────────

// TestGenerate_SameSeedDifferentCohorts_NoIDCollision guards against a bug where
// the UUID namespace was derived from the seed alone, so two cohorts sharing a
// seed produced identical primary keys (and the second insert failed).
func TestGenerate_SameSeedDifferentCohorts_NoIDCollision(t *testing.T) {
	p1 := defaultParams()
	p2 := defaultParams()
	p2.CohortID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	// Same seed, different cohort.
	if p1.Seed != p2.Seed {
		t.Fatalf("test setup: seeds differ (%d vs %d)", p1.Seed, p2.Seed)
	}

	r1, err := Generate(p1)
	if err != nil {
		t.Fatalf("Generate(c1): %v", err)
	}
	r2, err := Generate(p2)
	if err != nil {
		t.Fatalf("Generate(c2): %v", err)
	}

	seen := make(map[uuid.UUID]bool, len(r1.Entities))
	for _, e := range r1.Entities {
		seen[e.ID] = true
	}
	for _, e := range r2.Entities {
		if seen[e.ID] {
			t.Errorf("entity ID %s collides across cohorts with the same seed", e.ID)
		}
	}

	acctSeen := make(map[uuid.UUID]bool, len(r1.Accounts))
	for _, a := range r1.Accounts {
		acctSeen[a.ID] = true
	}
	for _, a := range r2.Accounts {
		if acctSeen[a.ID] {
			t.Errorf("account ID %s collides across cohorts with the same seed", a.ID)
		}
	}

	txSeen := make(map[uuid.UUID]bool, len(r1.Transactions))
	for _, tx := range r1.Transactions {
		txSeen[tx.ID] = true
	}
	for _, tx := range r2.Transactions {
		if txSeen[tx.ID] {
			t.Errorf("transaction ID %s collides across cohorts with the same seed", tx.ID)
		}
	}
}
