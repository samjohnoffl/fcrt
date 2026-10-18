package payment

import (
	"math"
	"time"
)

// StructuringThresholdCents is the canonical reporting threshold ($10,000).
// Transactions just below it are a structuring signal.
const StructuringThresholdCents int64 = 1_000_000

// Features are AML-relevant signals extracted from a payment message and its
// surrounding transaction history. All numeric fields are normalised to [0,1]
// so they can be combined into a single intelligence score.
type Features struct {
	// AmountBelowThreshold is 1.0 when the amount sits in the "structuring band"
	// just under the reporting threshold, else 0.0.
	AmountBelowThreshold float64

	// ThresholdProximity measures how close the amount is to the threshold,
	// 1.0 = exactly at the band edge, 0.0 = far away.
	ThresholdProximity float64

	// RoundAmount is 1.0 for round-number amounts (e.g. exactly $9,000).
	RoundAmount float64

	// Velocity is the normalised count of transfers between the same
	// debtor/creditor pair within the velocity window.
	Velocity float64

	// CounterpartyConcentration is the Herfindahl-style concentration of the
	// creditor across the debtor's recent payments (1.0 = single counterparty).
	CounterpartyConcentration float64

	// CrossBorder is 1.0 when debtor and creditor countries differ.
	CrossBorder float64

	// HighRiskFrom is 1.0 when the debtor's country is on the watch list.
	HighRiskFrom float64

	// ObfuscatedReference is 1.0 when remittance info looks deliberately vague
	// (empty or single generic token) despite a large amount.
	ObfuscatedReference float64

	// CryptoRail is 1.0 when the payment uses a crypto rail.
	CryptoRail float64
}

// IntelligenceScore combines the features into a single suspicion score in [0,1].
// Weights are intentionally simple and auditable; they are not machine-learned.
func (f Features) IntelligenceScore() float64 {
	score := 0.20*f.AmountBelowThreshold +
		0.15*f.ThresholdProximity +
		0.10*f.RoundAmount +
		0.15*f.Velocity +
		0.10*f.CounterpartyConcentration +
		0.10*f.CrossBorder +
		0.05*f.HighRiskFrom +
		0.05*f.ObfuscatedReference +
		0.10*f.CryptoRail
	return math.Min(1.0, math.Max(0.0, score))
}

// Context carries the surrounding history needed to compute velocity and
// concentration features.
type Context struct {
	// Window is the lookback window for velocity/concentration.
	Window time.Duration
	// RecentPayments are past payments by the same debtor (most recent first).
	RecentPayments []Message
	// HighRiskCountries is the set of ISO 3166-1 alpha-2 watch-list countries.
	HighRiskCountries map[string]bool
}

// Extract computes AML features for a payment message given its context.
func Extract(msg Message, ctx Context) Features {
	var f Features

	amt := msg.InterbankSettlementAmount.ValueCents
	threshold := StructuringThresholdCents

	// ── Structuring band: 50%..100% of the threshold ──
	lowerBand := threshold / 2
	if amt >= lowerBand && amt < threshold {
		f.AmountBelowThreshold = 1.0
		// Proximity: 1.0 at the threshold, 0.0 at the lower band edge.
		f.ThresholdProximity = float64(amt-lowerBand) / float64(threshold-lowerBand)
	}

	// ── Round amount (nearest $100, i.e. 10,000 cents) ──
	if amt > 0 && amt%10_000 == 0 {
		f.RoundAmount = 1.0
	}

	// ── Cross-border / high risk ──
	if msg.Debtor.Country != "" && msg.Creditor.Country != "" &&
		msg.Debtor.Country != msg.Creditor.Country {
		f.CrossBorder = 1.0
	}
	if ctx.HighRiskCountries != nil && ctx.HighRiskCountries[msg.Debtor.Country] {
		f.HighRiskFrom = 1.0
	}

	// ── Crypto rail ──
	if msg.InterbankSettlementAmount.Currency == "BTC" || msg.ChargeBearer == ChargeBearerDebtor {
		f.CryptoRail = 1.0
	}

	// ── Remittance obfuscation ──
	if msg.Remittance.Unstructured == "" && len(msg.Remittance.Structured) == 0 && amt >= lowerBand {
		f.ObfuscatedReference = 1.0
	}

	// ── Velocity + counterparty concentration from history ──
	window := ctx.Window
	if window <= 0 {
		window = 24 * time.Hour
	}
	creditorCounts := map[[16]byte]int{}
	var inWindow int
	for _, past := range ctx.RecentPayments {
		age := msg.ValueDate.Sub(past.ValueDate)
		if age < 0 || age > window {
			continue
		}
		inWindow++
		key := past.Creditor.AccountID
		creditorCounts[key]++
	}
	if len(ctx.RecentPayments) > 0 {
		// Velocity normalised against a soft cap of 10 payments/window.
		f.Velocity = math.Min(1.0, float64(inWindow)/10.0)

		// Herfindahl index over creditor distribution.
		total := 0
		for _, c := range creditorCounts {
			total += c
		}
		if total > 0 {
			var hhi float64
			for _, c := range creditorCounts {
				share := float64(c) / float64(total)
				hhi += share * share
			}
			f.CounterpartyConcentration = hhi
		}
	}

	return f
}

// clamp01 constrains v to [0,1].
func clamp01(v float64) float64 {
	return math.Min(1.0, math.Max(0.0, v))
}
