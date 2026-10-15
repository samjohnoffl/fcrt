// Package scenario defines types for Gemini-assisted scenario proposals.
package scenario

import (
	"time"

	"github.com/google/uuid"
)

// Status is the human-approval state of a proposal.
type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

// TxPatternStep describes one mutation instruction in a scenario.
// The schema intentionally stays loose (map) to accommodate diverse patterns;
// internal/experiment will parse into concrete mutation types.
type TxPatternStep map[string]any

// Proposal is a Gemini-generated scenario proposal awaiting human approval.
type Proposal struct {
	ID                     uuid.UUID      `json:"id"`
	CohortID               uuid.UUID      `json:"cohort_id"`
	Status                 Status         `json:"status"`
	ScenarioType           string         `json:"scenario_type"`
	Description            string         `json:"description"`
	ExpectedEvasionVector  string         `json:"expected_evasion_vector"`
	SuggestedTxPattern     []TxPatternStep `json:"suggested_tx_pattern"`
	GeminiPrompt           string         `json:"gemini_prompt"`
	GeminiResponse         string         `json:"gemini_response"`
	GeminiModel            string         `json:"gemini_model"`
	CorrelationID          uuid.UUID      `json:"correlation_id"`
	ApprovedBy             string         `json:"approved_by,omitempty"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
}
