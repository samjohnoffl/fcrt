// Package coverage defines types for AML rule coverage tracking.
package coverage

import (
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
)

// RuleCoverage tracks how often each AML rule has been exercised.
type RuleCoverage struct {
	ID              uuid.UUID  `json:"id"`
	RuleID          aml.RuleID `json:"rule_id"`
	RuleVersion     string     `json:"rule_version"`
	TimesTriggered  int        `json:"times_triggered"`
	LastTriggeredAt *time.Time `json:"last_triggered_at,omitempty"`
	CoverageWeight  float64    `json:"coverage_weight"` // higher = more deserving of next run
	UpdatedAt       time.Time  `json:"updated_at"`
}

// Summary is the aggregate coverage state returned by the API.
type Summary struct {
	TotalRules   int            `json:"total_rules"`
	CoveredRules int            `json:"covered_rules"` // times_triggered > 0
	CoveragePct  float64        `json:"coverage_pct"`  // 0.0–100.0
	Rules        []RuleCoverage `json:"rules"`
}
