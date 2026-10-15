// Package scenario — candidate.go
//
// Candidate is a Gemini-generated scenario proposal after schema validation.
// ScoreComponents records the four scoring dimensions used by the selector.
// RejectionRecord explains why a raw Gemini candidate was discarded.
package scenario

import (
	"time"

	"github.com/google/uuid"
)

// ─── Candidate types ─────────────────────────────────────────────────────────

// MutationType mirrors experiment.MutationType to avoid a circular import.
// Only the values listed in ApprovedMutationTypes are accepted; all others
// are rejected at the schema-validation boundary.
type MutationType string

const (
	MutationSplitTransaction MutationType = "split_transaction"
	MutationIntroduceShell   MutationType = "introduce_shell_entity"
	MutationDelayAndResubmit MutationType = "delay_and_resubmit"
)

// ApprovedMutationTypes is the exhaustive allowlist. Any value not in this
// set is rejected at parse time — model output is untrusted data.
var ApprovedMutationTypes = map[MutationType]bool{
	MutationSplitTransaction: true,
	MutationIntroduceShell:   true,
	MutationDelayAndResubmit: true,
}

// MutationSpec holds only the parameters allowed for a given mutation.
// Fields not listed here are rejected at validation time.
//
// Approved fields per mutation type:
//
//	split_transaction:     split_count (int, 2–20), threshold_cents (int64, >0)
//	delay_and_resubmit:    delay_minutes (int, 1–1440)
//	introduce_shell_entity: threshold_cents (int64, >0)
type MutationSpec struct {
	// SplitCount is the number of parts to split a single large transaction into.
	// Required for split_transaction; ignored otherwise.
	SplitCount int `json:"split_count,omitempty"`

	// ThresholdCents is the amount in cents above which a transaction is eligible
	// for splitting or shell-entity rerouting.
	// Required for split_transaction and introduce_shell_entity.
	ThresholdCents int64 `json:"threshold_cents,omitempty"`

	// DelayMinutes is the number of minutes to delay a transaction.
	// Required for delay_and_resubmit.
	DelayMinutes int `json:"delay_minutes,omitempty"`
}

// Candidate is a validated, scored scenario proposal produced by Gemini.
// It is ready for selection but has not yet been executed as an experiment pair.
type Candidate struct {
	// ID is assigned locally (not by Gemini) after validation.
	ID uuid.UUID `json:"id"`

	// CorrelationID is the request-level ID linking this candidate to the
	// Gemini API call that produced it. Required for AGENTS.md idempotency rule.
	CorrelationID uuid.UUID `json:"correlation_id"`

	// MutationType is the validated, approved mutation category.
	MutationType MutationType `json:"mutation_type"`

	// DeclaredChange is a human-readable description of what the variant changes.
	// Copied verbatim from the validated Gemini response.
	DeclaredChange string `json:"declared_change"`

	// MutationSpec holds the specific parameters for the mutation.
	MutationSpec MutationSpec `json:"mutation_spec"`

	// Rationale is Gemini's explanation of why this scenario is interesting.
	// Treated as free text; never executed or SQL-interpreted.
	Rationale string `json:"rationale"`

	// Tags is an optional set of short labels (e.g. ["high-risk", "cross-border"]).
	// Used for novelty scoring.
	Tags []string `json:"tags,omitempty"`

	// Score is the final composite score assigned by the selector.
	Score float64 `json:"score"`

	// ScoreComponents records the four raw component scores before weighting.
	ScoreComponents ScoreComponents `json:"score_components"`

	// GeneratedAt is when this candidate was received from Gemini.
	GeneratedAt time.Time `json:"generated_at"`
}

// FingerprintKey returns a string that uniquely identifies the semantic content
// of this candidate. Used for duplicate detection across proposals.
func (c *Candidate) FingerprintKey() string {
	return string(c.MutationType) + "|" + c.DeclaredChange
}

// ─── Scoring types ────────────────────────────────────────────────────────────

// ScoreComponents records the raw (pre-weight) value of each scoring dimension.
// All values are in [0.0, 1.0]. The weighted sum equals Candidate.Score.
type ScoreComponents struct {
	// Coverage measures how under-represented this mutation type is among
	// recently completed experiments. Range [0,1]; 1.0 = never tried.
	Coverage float64 `json:"coverage"`

	// Novelty measures how dissimilar this candidate is to the existing pool.
	// Computed as 1 − max(Jaccard similarity to any prior candidate).
	// Range [0,1]; 1.0 = completely novel tag set.
	Novelty float64 `json:"novelty"`

	// Validity is 1.0 if the candidate fully passes schema validation, 0.0
	// otherwise. Partially invalid candidates are rejected before scoring.
	Validity float64 `json:"validity"`

	// EIV (Expected Information Value) is Gemini's self-reported confidence
	// that this scenario would surface an evasion delta > 0. Parsed from the
	// rationale as a normalised float; defaults to 0.5 when absent.
	EIV float64 `json:"eiv"`
}

// ─── Rejection types ─────────────────────────────────────────────────────────

// RejectionReason classifies why a raw candidate was discarded.
type RejectionReason string

const (
	// RejectSchemaMissing means a required field is absent from the Gemini response.
	RejectSchemaMissing RejectionReason = "schema_missing_field"

	// RejectSchemaUnknownField means the Gemini response contained a field not
	// in the approved schema. Rejected to prevent prompt-injection side-channels.
	RejectSchemaUnknownField RejectionReason = "schema_unknown_field"

	// RejectSchemaInvalidValue means a field was present but its value is outside
	// the approved range or enum.
	RejectSchemaInvalidValue RejectionReason = "schema_invalid_value"

	// RejectUnapprovedMutation means the mutation_type is not in ApprovedMutationTypes.
	RejectUnapprovedMutation RejectionReason = "unapproved_mutation_type"

	// RejectDuplicate means an identical candidate (same fingerprint) was already
	// selected in this request.
	RejectDuplicate RejectionReason = "duplicate_candidate"

	// RejectMalformedJSON means the model response could not be parsed as JSON.
	RejectMalformedJSON RejectionReason = "malformed_json"

	// RejectBudgetExceeded means the per-request candidate budget was exhausted.
	RejectBudgetExceeded RejectionReason = "budget_exceeded"
)

// RejectionRecord explains why a specific raw Gemini output was discarded.
type RejectionRecord struct {
	// CorrelationID links to the Gemini request that produced the raw output.
	CorrelationID uuid.UUID `json:"correlation_id"`

	// Reason is the enumerated rejection category.
	Reason RejectionReason `json:"reason"`

	// Detail is a human-readable explanation, safe to log.
	// Must not include raw Gemini output that could contain injected content.
	Detail string `json:"detail"`

	// RejectedAt is when the rejection was recorded.
	RejectedAt time.Time `json:"rejected_at"`
}

// ─── Selection result ────────────────────────────────────────────────────────

// GenerationRequest is the input to a Gemini scenario generation call.
type GenerationRequest struct {
	// CorrelationID is assigned by the caller. All Gemini calls for this
	// request share the same ID (AGENTS.md idempotency requirement).
	CorrelationID uuid.UUID

	// CohortSummary is a structured description of the active cohort, used as
	// context in the Gemini prompt. Only statistical summaries are included;
	// no row-level data is sent to the model.
	CohortSummary CohortSummary

	// MaxCandidates is the maximum number of valid candidates to return.
	// Must be in [1, 10].
	MaxCandidates int

	// ExistingFingerprints is the set of FingerprintKey values already in the
	// pool, used for duplicate detection during this request.
	ExistingFingerprints map[string]bool
}

// CohortSummary is a statistical summary of a cohort, safe to include in
// a Gemini prompt. No individual-level data is ever sent.
type CohortSummary struct {
	EntityCount      int      `json:"entity_count"`
	AccountCount     int      `json:"account_count"`
	TxCount          int      `json:"tx_count"`
	WindowDays       int      `json:"window_days"`
	TopCountryCodes  []string `json:"top_country_codes"` // max 5
	AlertRatePercent float64  `json:"alert_rate_percent"`
}

// GenerationResult is the output of a Gemini generation call.
type GenerationResult struct {
	// Accepted contains validated, scored candidates ready for selection.
	Accepted []Candidate

	// Rejected contains records for every raw candidate that failed validation.
	Rejected []RejectionRecord

	// CorrelationID links this result to the originating request.
	CorrelationID uuid.UUID

	// PromptTokens and OutputTokens record usage for cost tracking.
	PromptTokens  int
	OutputTokens  int
}

// SelectionResult is the output of the adaptive selector.
type SelectionResult struct {
	// Selected is the ranked, deduplicated list of candidates chosen for experiments.
	Selected []Candidate

	// Rejected is the combined rejection log from Gemini validation + selector filtering.
	Rejected []RejectionRecord

	// WeightsVersion is the version string of the SelectionWeights used.
	WeightsVersion string
}
