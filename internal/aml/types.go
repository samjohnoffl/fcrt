// Package aml defines the mock AML harness types.
// The harness is stateless and deterministic: same input → same output always.
package aml

import (
	"time"

	"github.com/google/uuid"
)

// RuleID is a stable, unique identifier for an AML rule.
type RuleID string

const (
	RuleStructuring           RuleID = "structuring_v1"
	RuleLayering              RuleID = "layering_v1"
	RuleRapidMovement         RuleID = "rapid_movement_v1"
	RuleVelocity              RuleID = "velocity_v1"
	RuleCounterpartyExpansion RuleID = "counterparty_expansion_v1"
	RuleDormancyActivation    RuleID = "dormancy_activation_v1"
	RuleGeographicBand        RuleID = "geographic_band_v1"
	RuleOwnershipRelationship RuleID = "ownership_relationship_v1"
)

// AllRules lists every control family in the simulator, in stable order.
var AllRules = []RuleID{
	RuleStructuring,
	RuleLayering,
	RuleRapidMovement,
	RuleVelocity,
	RuleCounterpartyExpansion,
	RuleDormancyActivation,
	RuleGeographicBand,
	RuleOwnershipRelationship,
}

// OutcomeStatus is the five-state result of running one control.
//
// The distinction matters: "not triggered" claims the control examined the
// behaviour and found nothing, while "not applicable" means it could not
// examine it, and "inconclusive" means the available history was insufficient.
// A control error is always "failed", never "not triggered".
type OutcomeStatus string

const (
	OutcomeTriggered     OutcomeStatus = "triggered"
	OutcomeNotTriggered  OutcomeStatus = "not_triggered"
	OutcomeNotApplicable OutcomeStatus = "not_applicable"
	OutcomeInconclusive  OutcomeStatus = "inconclusive"
	OutcomeFailed        OutcomeStatus = "failed"
)

// ReasonCode explains why a control reached its status. It is deliberately
// coarse and stable, so coverage can be aggregated by reason.
type ReasonCode string

const (
	ReasonPatternDetected     ReasonCode = "pattern_detected"
	ReasonThresholdNotMet     ReasonCode = "threshold_not_met"
	ReasonRuleDisabled        ReasonCode = "rule_disabled"
	ReasonNoEvents            ReasonCode = "no_events"
	ReasonMissingField        ReasonCode = "missing_field"
	ReasonInsufficientHistory ReasonCode = "insufficient_history"
	ReasonEvaluationError     ReasonCode = "evaluation_error"
)

// RuleOutcome is the per-control result of one evaluation.
//
// EventIDs are the contributing transaction references, so a reviewer can trace
// a trigger (or the absence of one) back to specific synthetic events.
type RuleOutcome struct {
	RuleID      RuleID        `json:"rule_id"`
	RuleVersion string        `json:"rule_version"`
	Status      OutcomeStatus `json:"status"`
	ReasonCode  ReasonCode    `json:"reason_code"`
	EventIDs    []uuid.UUID   `json:"event_ids,omitempty"`
	AccountIDs  []uuid.UUID   `json:"account_ids,omitempty"`
	EntityIDs   []uuid.UUID   `json:"entity_ids,omitempty"`
	AmountCents int64         `json:"amount_cents,omitempty"`
	Currency    string        `json:"currency,omitempty"`
	Detail      string        `json:"detail,omitempty"`
}

// Alert represents a single AML rule trigger.
//
// The schema is a simplified version of the FinCEN SAR fields (PRODUCT_SPEC
// item 3): a subject, the involved accounts/transactions, the aggregate
// suspicious amount and currency, the suspicious-activity type, severity and
// a human-readable narrative. No real PII is ever present — IDs only.
type Alert struct {
	ID          uuid.UUID   `json:"id"`
	RuleID      RuleID      `json:"rule_id"`
	RuleVersion string      `json:"rule_version"`
	EntityID    uuid.UUID   `json:"entity_id"`
	AccountIDs  []uuid.UUID `json:"account_ids"`
	TxIDs       []uuid.UUID `json:"tx_ids"`
	// ── Simplified FinCEN SAR-aligned fields (PRODUCT_SPEC item 3) ──
	AmountCents  int64     `json:"amount_cents"`  // aggregate suspicious amount
	Currency     string    `json:"currency"`      // ISO 4217
	SubjectType  string    `json:"subject_type"`  // bank.EntityType of the subject
	ActivityType string    `json:"activity_type"` // structuring | layering | rapid_movement
	Severity     string    `json:"severity"`      // "low" | "medium" | "high"
	Description  string    `json:"description"`
	DetectedAt   time.Time `json:"detected_at"`
}

// HarnessResult is the complete, deterministic output of one harness run.
type HarnessResult struct {
	CohortID    uuid.UUID     `json:"cohort_id"`
	RuleVersion string        `json:"rule_version"`
	Alerts      []Alert       `json:"alerts"`
	Outcomes    []RuleOutcome `json:"outcomes"`
	RunAt       time.Time     `json:"run_at"`
}

// AlertDiff describes the difference between a baseline and a variant run.
type AlertDiff struct {
	BaselineAlertCount  int      `json:"baseline_alert_count"`
	VariantAlertCount   int      `json:"variant_alert_count"`
	EvasionDelta        int      `json:"evasion_delta"` // baseline - variant; positive = evasion
	NewAlertRuleIDs     []RuleID `json:"new_alert_rule_ids"`
	DroppedAlertRuleIDs []RuleID `json:"dropped_alert_rule_ids"`
}
