// Package aml — evaluator_test.go
//
// Unit tests for the AML Evaluator.
// All tests are in-memory; no database required.
package aml

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/bank"
)

// ─── Test fixtures ────────────────────────────────────────────────────────────

var (
	evalWindowStart = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	evalWindowEnd   = evalWindowStart.Add(90 * 24 * time.Hour)
)

// minimalSelection builds a BatchSelection with the given transactions
// plus a single entity and two accounts.
func minimalSelection(txs []bank.Transaction) *bank.BatchSelection {
	cohortID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	entityID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	acct1 := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	acct2 := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")

	entities := []bank.Entity{{
		ID: entityID, CohortID: cohortID, EntityType: bank.EntityTypeIndividual,
		Name: "Test", CountryCode: "US",
	}}
	accounts := []bank.Account{
		{ID: acct1, CohortID: cohortID, EntityID: entityID, AccountType: bank.AccountTypeChecking, Currency: bank.CurrencyUSD},
		{ID: acct2, CohortID: cohortID, EntityID: entityID, AccountType: bank.AccountTypeChecking, Currency: bank.CurrencyUSD},
	}

	return &bank.BatchSelection{
		CohortID:     cohortID,
		WindowStart:  evalWindowStart,
		WindowEnd:    evalWindowEnd,
		Manifest:     bank.CohortManifest{CohortID: cohortID},
		Entities:     entities,
		Accounts:     accounts,
		Transactions: txs,
	}
}

func makeTx(from, to uuid.UUID, amtCents int64, ts time.Time) bank.Transaction {
	return bank.Transaction{
		ID:            uuid.New(),
		CohortID:      uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		FromAccountID: from,
		ToAccountID:   to,
		AmountCents:   amtCents,
		Currency:      bank.CurrencyUSD,
		TxType:        bank.TxTypeACH,
		TxTimestamp:   ts,
		CreatedAt:     ts,
	}
}

func defaultEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	ev, err := NewEvaluator(DefaultControlConfig())
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	return ev
}

// ─── Determinism ─────────────────────────────────────────────────────────────

func TestEvaluator_Deterministic_EmptyInput(t *testing.T) {
	ev := defaultEvaluator(t)
	sel := minimalSelection(nil)

	r1 := ev.Evaluate(sel)
	r2 := ev.Evaluate(sel)

	if len(r1.Alerts) != len(r2.Alerts) {
		t.Errorf("alert count differs on repeat calls: %d vs %d", len(r1.Alerts), len(r2.Alerts))
	}
}

func TestEvaluator_Deterministic_WithTransactions(t *testing.T) {
	ev := defaultEvaluator(t)

	acct1 := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	acct2 := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	ts := evalWindowStart.Add(1 * time.Hour)

	// 4 transactions below structuring threshold: should fire structuring rule.
	var txs []bank.Transaction
	for i := 0; i < 4; i++ {
		txs = append(txs, makeTx(acct1, acct2, 900_000, ts.Add(time.Duration(i)*10*time.Minute)))
	}
	sel := minimalSelection(txs)

	r1 := ev.Evaluate(sel)
	r2 := ev.Evaluate(sel)

	if len(r1.Alerts) != len(r2.Alerts) {
		t.Errorf("non-deterministic alert count: %d vs %d", len(r1.Alerts), len(r2.Alerts))
	}
	for i := range r1.Alerts {
		if r1.Alerts[i].ID != r2.Alerts[i].ID {
			t.Errorf("alert[%d].ID differs on repeat call", i)
		}
	}
}

// ─── Structuring rule ─────────────────────────────────────────────────────────

func TestEvaluator_Structuring_Fires(t *testing.T) {
	ev := defaultEvaluator(t)
	acct1 := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	acct2 := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	ts := evalWindowStart.Add(1 * time.Hour)

	// Default threshold = $10,000 = 1_000_000 cents; min txs = 3.
	// Send 3 transactions of $9,999 each within 24h.
	var txs []bank.Transaction
	for i := 0; i < 3; i++ {
		txs = append(txs, makeTx(acct1, acct2, 999_900, ts.Add(time.Duration(i)*time.Hour)))
	}
	result := ev.Evaluate(minimalSelection(txs))

	found := false
	for _, a := range result.Alerts {
		if a.RuleID == RuleStructuring {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected structuring alert, got %d alerts: %+v", len(result.Alerts), result.Alerts)
	}
}

func TestEvaluator_Structuring_DoesNotFireBelowMinTx(t *testing.T) {
	ev := defaultEvaluator(t)
	acct1 := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	acct2 := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	ts := evalWindowStart.Add(1 * time.Hour)

	// Only 2 sub-threshold transactions; min is 3.
	txs := []bank.Transaction{
		makeTx(acct1, acct2, 999_900, ts),
		makeTx(acct1, acct2, 999_900, ts.Add(time.Hour)),
	}
	result := ev.Evaluate(minimalSelection(txs))
	for _, a := range result.Alerts {
		if a.RuleID == RuleStructuring {
			t.Errorf("unexpected structuring alert with only 2 transactions")
		}
	}
}

func TestEvaluator_Structuring_DoesNotFireAboveThreshold(t *testing.T) {
	ev := defaultEvaluator(t)
	acct1 := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	acct2 := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	ts := evalWindowStart.Add(1 * time.Hour)

	// Transactions above the reporting threshold — should NOT trigger structuring.
	var txs []bank.Transaction
	for i := 0; i < 4; i++ {
		txs = append(txs, makeTx(acct1, acct2, 1_500_000, ts.Add(time.Duration(i)*time.Hour)))
	}
	result := ev.Evaluate(minimalSelection(txs))
	for _, a := range result.Alerts {
		if a.RuleID == RuleStructuring {
			t.Error("unexpected structuring alert for above-threshold transactions")
		}
	}
}

func TestEvaluator_Structuring_DoesNotFireOutsideWindow(t *testing.T) {
	ev := defaultEvaluator(t)
	acct1 := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	acct2 := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	ts := evalWindowStart.Add(1 * time.Hour)

	// 3 sub-threshold transactions spread > 24h apart (default window).
	txs := []bank.Transaction{
		makeTx(acct1, acct2, 999_900, ts),
		makeTx(acct1, acct2, 999_900, ts.Add(25*time.Hour)),
		makeTx(acct1, acct2, 999_900, ts.Add(50*time.Hour)),
	}
	result := ev.Evaluate(minimalSelection(txs))
	for _, a := range result.Alerts {
		if a.RuleID == RuleStructuring {
			t.Error("unexpected structuring alert for transactions outside window")
		}
	}
}

// ─── Rapid movement rule ──────────────────────────────────────────────────────

func TestEvaluator_RapidMovement_Fires(t *testing.T) {
	ev := defaultEvaluator(t)
	acct1 := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	acct2 := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	ts := evalWindowStart.Add(1 * time.Hour)

	// Credit acct1 with $6,000 (above min $5,000).
	// Then debit acct1 of $5,500 (92%) within 60 minutes.
	txs := []bank.Transaction{
		makeTx(acct2, acct1, 600_000, ts),                     // credit acct1
		makeTx(acct1, acct2, 550_000, ts.Add(30*time.Minute)), // rapid debit
	}
	result := ev.Evaluate(minimalSelection(txs))

	found := false
	for _, a := range result.Alerts {
		if a.RuleID == RuleRapidMovement {
			found = true
		}
	}
	if !found {
		t.Errorf("expected rapid_movement alert, got %v", result.Alerts)
	}
}

func TestEvaluator_RapidMovement_DoesNotFireBelowRatio(t *testing.T) {
	ev := defaultEvaluator(t)
	acct1 := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	acct2 := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	ts := evalWindowStart.Add(1 * time.Hour)

	// Credit $6,000, debit only $1,200 (20%) — below 80% threshold.
	txs := []bank.Transaction{
		makeTx(acct2, acct1, 600_000, ts),
		makeTx(acct1, acct2, 120_000, ts.Add(30*time.Minute)),
	}
	result := ev.Evaluate(minimalSelection(txs))
	for _, a := range result.Alerts {
		if a.RuleID == RuleRapidMovement {
			t.Error("unexpected rapid_movement alert below ratio threshold")
		}
	}
}

func TestEvaluator_RapidMovement_DoesNotFireOutsideWindow(t *testing.T) {
	ev := defaultEvaluator(t)
	acct1 := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	acct2 := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	ts := evalWindowStart.Add(1 * time.Hour)

	// Credit $6,000, debit $5,500 but 3 hours later (> 60 min window).
	txs := []bank.Transaction{
		makeTx(acct2, acct1, 600_000, ts),
		makeTx(acct1, acct2, 550_000, ts.Add(3*time.Hour)),
	}
	result := ev.Evaluate(minimalSelection(txs))
	for _, a := range result.Alerts {
		if a.RuleID == RuleRapidMovement {
			t.Error("unexpected rapid_movement alert outside time window")
		}
	}
}

// ─── Alert ID determinism ─────────────────────────────────────────────────────

func TestEvaluator_AlertIDsDeterministic(t *testing.T) {
	ev := defaultEvaluator(t)
	acct1 := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	acct2 := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	ts := evalWindowStart.Add(1 * time.Hour)

	var txs []bank.Transaction
	for i := 0; i < 3; i++ {
		txs = append(txs, makeTx(acct1, acct2, 999_900, ts.Add(time.Duration(i)*time.Hour)))
	}
	sel := minimalSelection(txs)

	r1 := ev.Evaluate(sel)
	r2 := ev.Evaluate(sel)

	if len(r1.Alerts) != len(r2.Alerts) {
		t.Fatalf("alert count mismatch: %d vs %d", len(r1.Alerts), len(r2.Alerts))
	}
	for i := range r1.Alerts {
		if r1.Alerts[i].ID != r2.Alerts[i].ID {
			t.Errorf("alert[%d] ID not deterministic: %s vs %s",
				i, r1.Alerts[i].ID, r2.Alerts[i].ID)
		}
	}
}

// ─── Output sorted ────────────────────────────────────────────────────────────

func TestEvaluator_AlertsSortedByRuleID(t *testing.T) {
	ev := defaultEvaluator(t)
	acct1 := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	acct2 := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	ts := evalWindowStart.Add(1 * time.Hour)

	// Generate structuring conditions.
	var txs []bank.Transaction
	for i := 0; i < 3; i++ {
		txs = append(txs, makeTx(acct1, acct2, 999_900, ts.Add(time.Duration(i)*time.Hour)))
	}
	result := ev.Evaluate(minimalSelection(txs))

	for i := 1; i < len(result.Alerts); i++ {
		prev := string(result.Alerts[i-1].RuleID)
		curr := string(result.Alerts[i].RuleID)
		if curr < prev {
			t.Errorf("alerts not sorted at index %d: %q before %q", i, prev, curr)
		}
	}
}

// ─── Disabled rules ───────────────────────────────────────────────────────────

func TestEvaluator_DisabledRulesNotFired(t *testing.T) {
	// Build a config with only rapid_movement enabled.
	cfg, err := NewControlConfig(
		EnabledRules{RuleRapidMovement},
		StructuringConfig{ReportingThresholdCents: 1_000_000, WindowHours: 24, MinTransactions: 3},
		LayeringConfig{MinHops: 3, WindowHours: 72},
		RapidMovementConfig{MinAmountCents: 500_000, WindowMinutes: 60, DebitRatioPct: 80},
	)
	if err != nil {
		t.Fatalf("NewControlConfig: %v", err)
	}
	ev, _ := NewEvaluator(cfg)

	acct1 := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	acct2 := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	ts := evalWindowStart.Add(1 * time.Hour)

	// Create structuring conditions — should NOT fire because rule is disabled.
	var txs []bank.Transaction
	for i := 0; i < 4; i++ {
		txs = append(txs, makeTx(acct1, acct2, 999_900, ts.Add(time.Duration(i)*time.Hour)))
	}
	result := ev.Evaluate(minimalSelection(txs))
	for _, a := range result.Alerts {
		if a.RuleID == RuleStructuring {
			t.Error("structuring rule fired despite being disabled")
		}
	}
}

// ─── NewEvaluator validation ──────────────────────────────────────────────────

func TestNewEvaluator_RejectsInvalidConfig(t *testing.T) {
	cfg := DefaultControlConfig()
	cfg.ConfigHash = "" // invalidate
	_, err := NewEvaluator(cfg)
	if err == nil {
		t.Error("expected error for invalid config, got nil")
	}
}

// ─── RuleVersion in output ────────────────────────────────────────────────────

func TestEvaluator_RuleVersionMatchesConfigHash(t *testing.T) {
	ev := defaultEvaluator(t)
	result := ev.Evaluate(minimalSelection(nil))
	if result.RuleVersion != DefaultControlConfig().ConfigHash {
		t.Errorf("RuleVersion %q != ConfigHash %q", result.RuleVersion, DefaultControlConfig().ConfigHash)
	}
}

// ─── SAR-aligned alert fields (PRODUCT_SPEC item 3) ───────────────────────────

func TestEvaluator_AlertHasSARFields_Structuring(t *testing.T) {
	ev := defaultEvaluator(t)
	acct1 := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	acct2 := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	ts := evalWindowStart.Add(1 * time.Hour)

	// 3 × $9,999 = $29,997 total suspicious amount.
	var txs []bank.Transaction
	for i := 0; i < 3; i++ {
		txs = append(txs, makeTx(acct1, acct2, 999_900, ts.Add(time.Duration(i)*time.Hour)))
	}
	result := ev.Evaluate(minimalSelection(txs))

	var found *Alert
	for i := range result.Alerts {
		if result.Alerts[i].RuleID == RuleStructuring {
			found = &result.Alerts[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected structuring alert, got %d alerts", len(result.Alerts))
	}

	if found.AmountCents != 3*999_900 {
		t.Errorf("AmountCents = %d, want %d", found.AmountCents, 3*999_900)
	}
	if found.Currency != string(bank.CurrencyUSD) {
		t.Errorf("Currency = %q, want %q", found.Currency, bank.CurrencyUSD)
	}
	if found.SubjectType != string(bank.EntityTypeIndividual) {
		t.Errorf("SubjectType = %q, want %q", found.SubjectType, bank.EntityTypeIndividual)
	}
	if found.ActivityType != "structuring" {
		t.Errorf("ActivityType = %q, want structuring", found.ActivityType)
	}
	if len(found.TxIDs) != 3 {
		t.Errorf("TxIDs = %d, want 3", len(found.TxIDs))
	}
}
