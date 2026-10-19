// Package aml — outcomes_test.go
//
// Tests for the five-status control outcome model and the five control
// families added alongside it. Every test is in-memory and deterministic.
package aml

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/bank"
)

// outcomeFixture is a small synthetic bank with two entities and three
// accounts. Tests add transactions to exercise one control at a time.
type outcomeFixture struct {
	sel      *bank.BatchSelection
	subject  uuid.UUID // entity that performs the behaviour
	counter  uuid.UUID // second entity (high-risk geography tests)
	primary  uuid.UUID // account owned by subject
	linked   uuid.UUID // second account owned by subject
	other    uuid.UUID // account owned by counter
	end      time.Time
	start    time.Time
	cohortID uuid.UUID
}

func newOutcomeFixture() *outcomeFixture {
	f := &outcomeFixture{
		cohortID: uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		subject:  uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		counter:  uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		primary:  uuid.MustParse("44444444-4444-4444-8444-444444444444"),
		linked:   uuid.MustParse("55555555-5555-4555-8555-555555555555"),
		other:    uuid.MustParse("66666666-6666-4666-8666-666666666666"),
	}
	f.end = time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	f.start = f.end.Add(-90 * 24 * time.Hour)
	f.sel = &bank.BatchSelection{
		CohortID:    f.cohortID,
		WindowStart: f.start,
		WindowEnd:   f.end,
		Manifest:    bank.CohortManifest{CohortID: f.cohortID},
		Entities: []bank.Entity{
			{ID: f.subject, CohortID: f.cohortID, EntityType: bank.EntityTypeCompany, Name: "Subject", CountryCode: "US"},
			{ID: f.counter, CohortID: f.cohortID, EntityType: bank.EntityTypeShell, Name: "Counter", CountryCode: "PA"},
		},
		Accounts: []bank.Account{
			{ID: f.primary, CohortID: f.cohortID, EntityID: f.subject, AccountType: bank.AccountTypeChecking, Currency: bank.CurrencyUSD},
			{ID: f.linked, CohortID: f.cohortID, EntityID: f.subject, AccountType: bank.AccountTypeSavings, Currency: bank.CurrencyUSD},
			{ID: f.other, CohortID: f.cohortID, EntityID: f.counter, AccountType: bank.AccountTypeChecking, Currency: bank.CurrencyUSD},
		},
	}
	return f
}

func (f *outcomeFixture) addTx(from, to uuid.UUID, amount int64, ts time.Time) bank.Transaction {
	tx := bank.Transaction{
		ID:            uuid.New(),
		CohortID:      f.cohortID,
		FromAccountID: from,
		ToAccountID:   to,
		AmountCents:   amount,
		Currency:      bank.CurrencyUSD,
		TxType:        bank.TxTypeACH,
		TxTimestamp:   ts,
		CreatedAt:     ts,
	}
	f.sel.Transactions = append(f.sel.Transactions, tx)
	return tx
}

func outcomeFor(t *testing.T, outcomes []RuleOutcome, id RuleID) RuleOutcome {
	t.Helper()
	for _, oc := range outcomes {
		if oc.RuleID == id {
			return oc
		}
	}
	t.Fatalf("no outcome for rule %s (got %d outcomes)", id, len(outcomes))
	return RuleOutcome{}
}

func TestEvaluate_ProducesOutcomeForEveryControl(t *testing.T) {
	f := newOutcomeFixture()
	res := defaultEvaluator(t).Evaluate(f.sel)

	if len(res.Outcomes) != len(AllRules) {
		t.Fatalf("outcomes = %d, want %d (one per control family)", len(res.Outcomes), len(AllRules))
	}
	seen := make(map[RuleID]bool)
	for _, oc := range res.Outcomes {
		if oc.RuleVersion != res.RuleVersion {
			t.Errorf("outcome %s rule_version=%q, want %q", oc.RuleID, oc.RuleVersion, res.RuleVersion)
		}
		if oc.Status == "" {
			t.Errorf("outcome %s has empty status", oc.RuleID)
		}
		seen[oc.RuleID] = true
	}
	for _, id := range AllRules {
		if !seen[id] {
			t.Errorf("missing outcome for %s", id)
		}
	}
}

func TestEvaluate_EmptySelectionIsNotApplicable(t *testing.T) {
	f := newOutcomeFixture()
	res := defaultEvaluator(t).Evaluate(f.sel)

	for _, oc := range res.Outcomes {
		if oc.Status != OutcomeNotApplicable {
			t.Errorf("%s status=%q, want not_applicable", oc.RuleID, oc.Status)
		}
		if oc.ReasonCode != ReasonNoEvents {
			t.Errorf("%s reason=%q, want no_events", oc.RuleID, oc.ReasonCode)
		}
	}
}

func TestEvaluate_DisabledControlIsNotApplicable(t *testing.T) {
	base := DefaultControlConfig()
	cfg, err := NewControlConfig(EnabledRules{RuleStructuring}, base.Structuring, base.Layering, base.RapidMovement)
	if err != nil {
		t.Fatalf("NewControlConfig: %v", err)
	}
	ev, err := NewEvaluator(cfg)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	res := ev.Evaluate(newOutcomeFixture().sel)

	oc := outcomeFor(t, res.Outcomes, RuleVelocity)
	if oc.Status != OutcomeNotApplicable || oc.ReasonCode != ReasonRuleDisabled {
		t.Errorf("disabled velocity status=%q reason=%q, want not_applicable/rule_disabled", oc.Status, oc.ReasonCode)
	}
	// The enabled rule is evaluated (and here finds no events).
	oc = outcomeFor(t, res.Outcomes, RuleStructuring)
	if oc.Status != OutcomeNotApplicable || oc.ReasonCode != ReasonNoEvents {
		t.Errorf("enabled structuring status=%q reason=%q, want not_applicable/no_events", oc.Status, oc.ReasonCode)
	}
}

func TestControlErrorIsFailedNeverNotTriggered(t *testing.T) {
	ev := defaultEvaluator(t)
	oc, alerts := ev.runRuleSafe(RuleID("does_not_exist"), newOutcomeFixture().sel)

	if oc.Status != OutcomeFailed {
		t.Fatalf("status=%q, want failed (a control error must never be not_triggered)", oc.Status)
	}
	if oc.ReasonCode != ReasonEvaluationError {
		t.Errorf("reason=%q, want evaluation_error", oc.ReasonCode)
	}
	if len(alerts) != 0 {
		t.Errorf("alerts=%d, want 0", len(alerts))
	}
}

func TestVelocity_TriggersOnCountIncrease(t *testing.T) {
	f := newOutcomeFixture()
	// Baseline window: [end-48h, end-24h) -> one transaction.
	f.addTx(f.primary, f.other, 100_000, f.end.Add(-40*time.Hour))
	// Evaluation window: [end-24h, end) -> four transactions (rise of 3).
	for i := 0; i < 4; i++ {
		f.addTx(f.primary, f.other, 100_000, f.end.Add(-time.Duration(20-i)*time.Hour))
	}

	res := defaultEvaluator(t).Evaluate(f.sel)
	oc := outcomeFor(t, res.Outcomes, RuleVelocity)
	if oc.Status != OutcomeTriggered {
		t.Fatalf("velocity status=%q reason=%q, want triggered", oc.Status, oc.ReasonCode)
	}
	if len(oc.EventIDs) == 0 {
		t.Error("triggered velocity outcome has no contributing event IDs")
	}
	if oc.AmountCents == 0 {
		t.Error("triggered velocity outcome has no amount")
	}
}

func TestVelocity_DoesNotTriggerOnStableActivity(t *testing.T) {
	f := newOutcomeFixture()
	for i := 0; i < 4; i++ {
		ts := f.end.Add(-time.Duration(40-i) * time.Hour)
		f.addTx(f.primary, f.other, 100_000, ts)
	}
	res := defaultEvaluator(t).Evaluate(f.sel)
	oc := outcomeFor(t, res.Outcomes, RuleVelocity)
	if oc.Status == OutcomeTriggered {
		t.Fatalf("velocity triggered on stable activity: %s", oc.Detail)
	}
}

func TestCounterpartyExpansion_TriggersOnNewCounterparties(t *testing.T) {
	f := newOutcomeFixture()
	// Baseline window has a single counterparty.
	f.addTx(f.primary, f.other, 50_000, f.end.Add(-100*time.Hour))
	// Evaluation window (last 72h) has four distinct counterparties.
	for i := 0; i < 4; i++ {
		f.addTx(f.primary, uuid.New(), 50_000, f.end.Add(-time.Duration(10-i)*time.Hour))
	}

	res := defaultEvaluator(t).Evaluate(f.sel)
	oc := outcomeFor(t, res.Outcomes, RuleCounterpartyExpansion)
	if oc.Status != OutcomeTriggered {
		t.Fatalf("counterparty expansion status=%q reason=%q, want triggered", oc.Status, oc.ReasonCode)
	}
	if len(oc.EventIDs) < 4 {
		t.Errorf("event IDs=%d, want >= 4", len(oc.EventIDs))
	}
}

func TestDormancyActivation_TriggersAfterGap(t *testing.T) {
	f := newOutcomeFixture()
	f.addTx(f.primary, f.other, 50_000, f.end.Add(-60*24*time.Hour))
	f.addTx(f.primary, f.other, 200_000, f.end.Add(-5*24*time.Hour))

	res := defaultEvaluator(t).Evaluate(f.sel)
	oc := outcomeFor(t, res.Outcomes, RuleDormancyActivation)
	if oc.Status != OutcomeTriggered {
		t.Fatalf("dormancy status=%q reason=%q, want triggered", oc.Status, oc.ReasonCode)
	}
	if len(oc.EventIDs) != 2 {
		t.Errorf("event IDs=%d, want 2 (last pre-gap and first post-gap)", len(oc.EventIDs))
	}
}

func TestDormancyActivation_InconclusiveWithoutHistory(t *testing.T) {
	f := newOutcomeFixture()
	f.addTx(f.primary, f.other, 50_000, f.end.Add(-time.Hour))

	res := defaultEvaluator(t).Evaluate(f.sel)
	oc := outcomeFor(t, res.Outcomes, RuleDormancyActivation)
	if oc.Status != OutcomeInconclusive {
		t.Fatalf("dormancy status=%q reason=%q, want inconclusive (one event is not history)", oc.Status, oc.ReasonCode)
	}
	if oc.ReasonCode != ReasonInsufficientHistory {
		t.Errorf("reason=%q, want insufficient_history", oc.ReasonCode)
	}
}

func TestGeographicBand_TriggersOnHighRiskCounterparty(t *testing.T) {
	f := newOutcomeFixture()
	f.addTx(f.primary, f.other, 75_000, f.end.Add(-10*time.Hour))
	f.addTx(f.primary, f.other, 75_000, f.end.Add(-9*time.Hour))

	res := defaultEvaluator(t).Evaluate(f.sel)
	oc := outcomeFor(t, res.Outcomes, RuleGeographicBand)
	if oc.Status != OutcomeTriggered {
		t.Fatalf("geographic band status=%q reason=%q, want triggered", oc.Status, oc.ReasonCode)
	}
}

func TestGeographicBand_DoesNotTriggerOnDomesticFlow(t *testing.T) {
	f := newOutcomeFixture()
	f.sel.Entities[0].CountryCode = "US"
	f.sel.Entities[1].CountryCode = "US"
	f.addTx(f.primary, f.other, 75_000, f.end.Add(-10*time.Hour))
	f.addTx(f.primary, f.other, 75_000, f.end.Add(-9*time.Hour))

	res := defaultEvaluator(t).Evaluate(f.sel)
	oc := outcomeFor(t, res.Outcomes, RuleGeographicBand)
	if oc.Status == OutcomeTriggered {
		t.Fatalf("geographic band triggered on domestic flow: %s", oc.Detail)
	}
}

func TestOwnershipRelationship_TriggersOnLinkedAccountFlow(t *testing.T) {
	f := newOutcomeFixture()
	for i := 0; i < 3; i++ {
		f.addTx(f.primary, f.linked, 40_000, f.end.Add(-time.Duration(6-i)*time.Hour))
	}

	res := defaultEvaluator(t).Evaluate(f.sel)
	oc := outcomeFor(t, res.Outcomes, RuleOwnershipRelationship)
	if oc.Status != OutcomeTriggered {
		t.Fatalf("ownership status=%q reason=%q, want triggered", oc.Status, oc.ReasonCode)
	}
	if len(oc.AccountIDs) != 2 {
		t.Errorf("account IDs=%d, want 2 linked accounts", len(oc.AccountIDs))
	}
}

func TestOwnershipRelationship_NotTriggeredAcrossEntities(t *testing.T) {
	f := newOutcomeFixture()
	for i := 0; i < 3; i++ {
		f.addTx(f.primary, f.other, 40_000, f.end.Add(-time.Duration(6-i)*time.Hour))
	}

	res := defaultEvaluator(t).Evaluate(f.sel)
	oc := outcomeFor(t, res.Outcomes, RuleOwnershipRelationship)
	if oc.Status == OutcomeTriggered {
		t.Fatalf("ownership triggered across distinct entities: %s", oc.Detail)
	}
}
