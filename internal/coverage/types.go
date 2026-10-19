// Package coverage defines types for AML coverage tracking.
//
// Three maps are tracked separately because they answer different questions:
//
//   - Control coverage: has each control family been exercised at all?
//   - Outcome coverage:  how does each control respond (triggered, clean,
//     not applicable, inconclusive, failed)?
//   - Scenario coverage: which genome dimensions and values have been probed?
//
// Collapsing them into one percentage would let a control that fires randomly
// look "covered". Keeping them separate keeps the claim honest.
package coverage

import (
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
)

// RuleCoverage tracks how often each AML control has been evaluated and with
// which result. TimesTriggered is kept for backwards compatibility with the
// original coverage endpoint.
type RuleCoverage struct {
	ID                 uuid.UUID  `json:"id"`
	RuleID             aml.RuleID `json:"rule_id"`
	RuleVersion        string     `json:"rule_version"`
	TimesTriggered     int        `json:"times_triggered"`
	TimesNotTriggered  int        `json:"times_not_triggered"`
	TimesNotApplicable int        `json:"times_not_applicable"`
	TimesInconclusive  int        `json:"times_inconclusive"`
	TimesFailed        int        `json:"times_failed"`
	LastStatus         string     `json:"last_status,omitempty"`
	LastTriggeredAt    *time.Time `json:"last_triggered_at,omitempty"`
	LastEvaluatedAt    *time.Time `json:"last_evaluated_at,omitempty"`
	CoverageWeight     float64    `json:"coverage_weight"` // higher = more deserving of next run
	UpdatedAt          time.Time  `json:"updated_at"`
}

// ScenarioCoverage records one genome dimension value that has been exercised.
type ScenarioCoverage struct {
	Dimension  string    `json:"dimension"`
	Value      string    `json:"value"`
	TimesSeen  int       `json:"times_seen"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// OutcomeCoverage is the rule x status cross-tab.
type OutcomeCoverage struct {
	RuleID     aml.RuleID        `json:"rule_id"`
	Status     aml.OutcomeStatus `json:"status"`
	TimesSeen  int               `json:"times_seen"`
	LastSeenAt time.Time         `json:"last_seen_at"`
}

// Summary is the aggregate coverage state returned by the API.
type Summary struct {
	TotalRules   int            `json:"total_rules"`
	CoveredRules int            `json:"covered_rules"` // times_triggered > 0
	CoveragePct  float64        `json:"coverage_pct"`  // 0.0–100.0
	Rules        []RuleCoverage `json:"rules"`
	// Scenario is the genome-dimension map. Empty until an experiment is linked
	// to an approved scenario proposal.
	Scenario []ScenarioCoverage `json:"scenario"`
	// Outcomes is the rule x status map for the five-status model.
	Outcomes []OutcomeCoverage `json:"outcomes"`
}
