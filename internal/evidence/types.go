// Package evidence defines the Evidence Bundle type and its schema.
// An evidence bundle is an immutable, self-contained artifact produced
// at the end of each experiment run. It must be reproducible: given the
// same inputs, the same bundle is always generated.
package evidence

import (
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/coverage"
	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

// GeminiTrace captures the full LLM call associated with a scenario proposal.
type GeminiTrace struct {
	CorrelationID uuid.UUID `json:"correlation_id"`
	Model         string    `json:"model"`
	Prompt        string    `json:"prompt"`
	Response      string    `json:"response"`
	CalledAt      time.Time `json:"called_at"`
}

// CohortSnapshot is a point-in-time copy of the cohort that was used.
type CohortSnapshot struct {
	CohortID    uuid.UUID          `json:"cohort_id"`
	Seed        int64              `json:"seed"`
	Predicate   bank.Predicate     `json:"predicate"`
	EntityCount int                `json:"entity_count"`
	TxCount     int                `json:"tx_count"`
	LockedAt    time.Time          `json:"locked_at"`
}

// Bundle is the complete, self-contained evidence artifact for one experiment.
// It is stored as JSONB in `evidence_bundle.bundle` and can be exported to JSON.
type Bundle struct {
	RunID           uuid.UUID              `json:"run_id"`          // = experiment.id
	AMLRuleVersion  string                 `json:"aml_rule_version"`
	CohortSnapshot  CohortSnapshot         `json:"cohort_snapshot"`
	ScenarioParams  *scenario.Proposal     `json:"scenario_params,omitempty"`
	BaselineAlerts  []aml.Alert            `json:"baseline_alerts"`
	VariantAlerts   []aml.Alert            `json:"variant_alerts"`
	AMLDiff         aml.AlertDiff          `json:"aml_diff"`
	CoverageDelta   []coverage.RuleCoverage `json:"coverage_delta"` // rules whose counts changed
	GeminiTrace     *GeminiTrace           `json:"gemini_trace,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	// Checksum is SHA-256 of the JSON encoding of Bundle with Checksum="" (canonical form).
	Checksum        string                 `json:"checksum"`
}
