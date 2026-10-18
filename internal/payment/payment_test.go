package payment

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/bank"
)

func mkEntity(country string, et bank.EntityType) bank.Entity {
	return bank.Entity{ID: uuid.New(), CountryCode: country, Name: "ACME " + country, EntityType: et}
}

func mkAccount() bank.Account {
	return bank.Account{ID: uuid.New(), Currency: "USD"}
}

func mkTx(amountCents int64, ref string) bank.Transaction {
	return bank.Transaction{
		ID:          uuid.New(),
		AmountCents: amountCents,
		Currency:    "USD",
		TxType:      bank.TxTypeWire,
		TxTimestamp: time.Now().UTC(),
		Reference:   ref,
	}
}

func TestNormalizeMapsCoreFields(t *testing.T) {
	tx := mkTx(900_000, "INV=12345")
	from := mkAccount()
	to := mkAccount()
	msg := Normalize(tx, from, mkEntity("US", bank.EntityTypeIndividual), to, mkEntity("GB", bank.EntityTypeCompany))

	if msg.SourceTxID != tx.ID {
		t.Error("source tx id not preserved")
	}
	if msg.InterbankSettlementAmount.ValueCents != 900_000 {
		t.Errorf("amount = %d, want 900000", msg.InterbankSettlementAmount.ValueCents)
	}
	if msg.Debtor.AccountID != from.ID || msg.Creditor.AccountID != to.ID {
		t.Error("debtor/creditor account mismatch")
	}
	if msg.MsgID == "" {
		t.Error("MsgID empty")
	}
	if msg.ChargeBearer != ChargeBearerShared {
		t.Errorf("charge bearer = %s, want SHAR", msg.ChargeBearer)
	}
}

func TestNormalizeRemittanceStructured(t *testing.T) {
	ri := normalizeRemittance("INV=123;PO=456")
	if ri.Structured == nil || ri.Structured["INV"] != "123" || ri.Structured["PO"] != "456" {
		t.Errorf("structured parse failed: %+v", ri)
	}
}

func TestNormalizeRemittanceUnstructured(t *testing.T) {
	ri := normalizeRemittance("payment for consulting services")
	if ri.Unstructured == "" {
		t.Error("expected unstructured remittance")
	}
	if ri.Structured != nil {
		t.Error("expected no structured remittance")
	}
}

func TestNormalizeRemittanceEmpty(t *testing.T) {
	ri := normalizeRemittance("")
	if ri.Unstructured != "" || ri.Structured != nil {
		t.Errorf("expected empty remittance, got %+v", ri)
	}
}

func TestFeaturesStructuringBand(t *testing.T) {
	// $9,500 -> within the 50%..100% structuring band.
	msg := Message{InterbankSettlementAmount: Amount{ValueCents: 950_000, Currency: "USD"}}
	f := Extract(msg, Context{})
	if f.AmountBelowThreshold != 1.0 {
		t.Errorf("amount below threshold = %f, want 1.0", f.AmountBelowThreshold)
	}
	if f.ThresholdProximity <= 0 || f.ThresholdProximity > 1 {
		t.Errorf("threshold proximity out of range: %f", f.ThresholdProximity)
	}
}

func TestFeaturesAboveThresholdNotStructuring(t *testing.T) {
	msg := Message{InterbankSettlementAmount: Amount{ValueCents: 2_000_000, Currency: "USD"}}
	f := Extract(msg, Context{})
	if f.AmountBelowThreshold != 0.0 {
		t.Errorf("amount below threshold = %f, want 0.0", f.AmountBelowThreshold)
	}
}

func TestFeaturesRoundAmount(t *testing.T) {
	msg := Message{InterbankSettlementAmount: Amount{ValueCents: 900_000, Currency: "USD"}}
	f := Extract(msg, Context{})
	if f.RoundAmount != 1.0 {
		t.Errorf("round amount = %f, want 1.0", f.RoundAmount)
	}
}

func TestFeaturesCrossBorder(t *testing.T) {
	msg := Message{
		Debtor:   Party{Country: "US"},
		Creditor: Party{Country: "RU"},
	}
	f := Extract(msg, Context{})
	if f.CrossBorder != 1.0 {
		t.Errorf("cross border = %f, want 1.0", f.CrossBorder)
	}
}

func TestFeaturesHighRiskFrom(t *testing.T) {
	msg := Message{Debtor: Party{Country: "RU"}, Creditor: Party{Country: "RU"}}
	f := Extract(msg, Context{HighRiskCountries: map[string]bool{"RU": true}})
	if f.HighRiskFrom != 1.0 {
		t.Errorf("high risk from = %f, want 1.0", f.HighRiskFrom)
	}
}

func TestFeaturesVelocityAndConcentration(t *testing.T) {
	now := time.Now().UTC()
	creditor := mkAccount()
	history := []Message{
		{ValueDate: now.Add(-1 * time.Hour), Creditor: Party{AccountID: creditor.ID}},
		{ValueDate: now.Add(-2 * time.Hour), Creditor: Party{AccountID: creditor.ID}},
		{ValueDate: now.Add(-3 * time.Hour), Creditor: Party{AccountID: creditor.ID}},
	}
	msg := Message{ValueDate: now, Creditor: Party{AccountID: creditor.ID}}
	f := Extract(msg, Context{Window: 24 * time.Hour, RecentPayments: history})
	if f.Velocity <= 0 {
		t.Errorf("velocity = %f, want > 0", f.Velocity)
	}
	// All same creditor -> concentration 1.0
	if f.CounterpartyConcentration != 1.0 {
		t.Errorf("concentration = %f, want 1.0", f.CounterpartyConcentration)
	}
}

func TestFeaturesVelocityWindowExcludesOld(t *testing.T) {
	now := time.Now().UTC()
	history := []Message{
		{ValueDate: now.Add(-72 * time.Hour), Creditor: Party{AccountID: uuid.New()}},
	}
	msg := Message{ValueDate: now}
	f := Extract(msg, Context{Window: 24 * time.Hour, RecentPayments: history})
	if f.Velocity != 0 {
		t.Errorf("velocity = %f, want 0 (outside window)", f.Velocity)
	}
}

func TestIntelligenceScoreRange(t *testing.T) {
	cases := []Features{
		{},
		{AmountBelowThreshold: 1, ThresholdProximity: 1, RoundAmount: 1, Velocity: 1, CounterpartyConcentration: 1, CrossBorder: 1, HighRiskFrom: 1, ObfuscatedReference: 1, CryptoRail: 1},
	}
	for i, f := range cases {
		s := f.IntelligenceScore()
		if s < 0 || s > 1 {
			t.Errorf("case %d score out of range: %f", i, s)
		}
	}
}

func TestAnalyzeBatchSortedDescending(t *testing.T) {
	suspicious := Message{
		MsgID:                     "M-SUSP",
		InterbankSettlementAmount: Amount{ValueCents: 950_000, Currency: "USD"},
		Debtor:                    Party{Country: "RU"},
		Creditor:                  Party{Country: "RU"},
	}
	benign := Message{
		MsgID:                     "M-BENIGN",
		InterbankSettlementAmount: Amount{ValueCents: 12_345, Currency: "USD"},
		Debtor:                    Party{Country: "US"},
		Creditor:                  Party{Country: "US"},
	}
	out := AnalyzeBatch([]Message{benign, suspicious}, Context{HighRiskCountries: map[string]bool{"RU": true}})
	if len(out) != 2 {
		t.Fatalf("got %d, want 2", len(out))
	}
	if out[0].Message.MsgID != "M-SUSP" {
		t.Errorf("expected suspicious first, got %s", out[0].Message.MsgID)
	}
}

func TestSummarize(t *testing.T) {
	scored := []Scored{
		{Score: 0.8, Features: Features{AmountBelowThreshold: 1, CrossBorder: 1}},
		{Score: 0.2},
	}
	s := Summarize(scored)
	if s.Total != 2 || s.HighRisk != 1 || s.Structuring != 1 || s.CrossBorder != 1 {
		t.Errorf("unexpected summary: %+v", s)
	}
	if s.MeanScore <= 0 || s.MaxScore != 0.8 {
		t.Errorf("bad aggregate scores: %+v", s)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	s := Summarize(nil)
	if s.Total != 0 || s.MeanScore != 0 {
		t.Errorf("empty summary should be zero, got %+v", s)
	}
}
