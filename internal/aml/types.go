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
	RuleStructuring   RuleID = "structuring_v1"
	RuleLayering      RuleID = "layering_v1"
	RuleRapidMovement RuleID = "rapid_movement_v1"
)

// Alert represents a single AML rule trigger.
type Alert struct {
	ID          uuid.UUID `json:"id"`
	RuleID      RuleID    `json:"rule_id"`
	RuleVersion string    `json:"rule_version"`
	EntityID    uuid.UUID `json:"entity_id"`
	AccountIDs  []uuid.UUID `json:"account_ids"`
	TxIDs       []uuid.UUID `json:"tx_ids"`
	Severity    string    `json:"severity"` // "low" | "medium" | "high"
	Description string    `json:"description"`
	DetectedAt  time.Time `json:"detected_at"`
}

// HarnessResult is the complete, deterministic output of one harness run.
type HarnessResult struct {
	CohortID    uuid.UUID `json:"cohort_id"`
	RuleVersion string    `json:"rule_version"`
	Alerts      []Alert   `json:"alerts"`
	RunAt       time.Time `json:"run_at"`
}

// AlertDiff describes the difference between a baseline and a variant run.
type AlertDiff struct {
	BaselineAlertCount    int      `json:"baseline_alert_count"`
	VariantAlertCount     int      `json:"variant_alert_count"`
	EvasionDelta          int      `json:"evasion_delta"` // baseline - variant; positive = evasion
	NewAlertRuleIDs       []RuleID `json:"new_alert_rule_ids"`
	DroppedAlertRuleIDs   []RuleID `json:"dropped_alert_rule_ids"`
}
