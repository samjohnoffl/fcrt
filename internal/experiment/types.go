// Package experiment — types.go
//
// Full type definitions for the experiment layer.
// An experiment is a pair: one baseline run (unmodified input) and one
// variant run (input with a declared mutation applied). Both members of
// a pair MUST use the same ControlConfig (verified via ConfigHash) and
// the same frozen input context (verified via ManifestHash).
package experiment

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
	"github.com/yourorg/financial-crime-red-team/internal/bank"
)

// ─── Mutation types ───────────────────────────────────────────────────────────

// MutationType identifies the kind of synthetic mutation applied to the variant.
type MutationType string

const (
	MutationSplitTransaction MutationType = "split_transaction"
	MutationIntroduceShell   MutationType = "introduce_shell_entity"
	MutationDelayAndResubmit MutationType = "delay_and_resubmit"
	// MutationNone is used for the baseline run (no mutation).
	MutationNone MutationType = "none"
)

// Status tracks the lifecycle of a pair.
type Status string

const (
	StatusPending      Status = "pending"
	StatusQueued       Status = "queued"
	StatusRunning      Status = "running"
	StatusCompleted    Status = "completed"
	StatusFailed       Status = "failed"
	StatusCancelled    Status = "cancelled"
	StatusInconclusive Status = "inconclusive"
)

// ─── Input context ────────────────────────────────────────────────────────────

// InputContext is the frozen, hashed input to an experiment pair.
// Both runs in a pair must reference the same InputContext.
// ManifestHash is computed from the CohortManifest JSON; it will change
// if the manifest changes (e.g. a re-generation), making the pair incomparable.
type InputContext struct {
	CohortID     uuid.UUID           `json:"cohort_id"`
	ManifestHash string              `json:"manifest_hash"`
	Manifest     bank.CohortManifest `json:"manifest"`
	WindowStart  time.Time           `json:"window_start"`
	WindowEnd    time.Time           `json:"window_end"`
}

// NewInputContext constructs an InputContext and computes the ManifestHash.
func NewInputContext(manifest bank.CohortManifest, windowStart, windowEnd time.Time) (InputContext, error) {
	b, err := json.Marshal(manifest)
	if err != nil {
		return InputContext{}, fmt.Errorf("experiment: marshal manifest: %w", err)
	}
	sum := sha256.Sum256(b)
	return InputContext{
		CohortID:     manifest.CohortID,
		ManifestHash: fmt.Sprintf("%x", sum),
		Manifest:     manifest,
		WindowStart:  windowStart,
		WindowEnd:    windowEnd,
	}, nil
}

// ─── Pair specification ───────────────────────────────────────────────────────

// PairSpec is the specification for creating a new experiment pair.
// It is validated before any work is performed.
type PairSpec struct {
	// Name is a human label for the pair.
	Name string
	// Input is the frozen, hashed input context.
	Input InputContext
	// Config is the versioned AML control configuration.
	// Its ConfigHash is recorded and verified.
	Config *aml.ControlConfig
	// DeclaredChange is a human description of what the variant changes.
	DeclaredChange string
	// MutationType is the kind of mutation to apply.
	MutationType MutationType
	// MutationSpec holds the full mutation parameters (flexible JSON blob).
	// any is acceptable here: this is an open-schema config blob for mutations.
	MutationSpec map[string]any
}

// Validate returns an error if the spec is incomplete or contradictory.
func (ps *PairSpec) Validate() error {
	if ps.Name == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidPairSpec)
	}
	if ps.Input.CohortID == uuid.Nil {
		return fmt.Errorf("%w: input.cohort_id is nil", ErrInvalidPairSpec)
	}
	if ps.Input.ManifestHash == "" {
		return fmt.Errorf("%w: input.manifest_hash is empty", ErrInvalidPairSpec)
	}
	if ps.Input.WindowEnd.IsZero() || ps.Input.WindowStart.IsZero() {
		return fmt.Errorf("%w: window times must be set", ErrInvalidPairSpec)
	}
	if !ps.Input.WindowEnd.After(ps.Input.WindowStart) {
		return fmt.Errorf("%w: window_end must be after window_start", ErrInvalidPairSpec)
	}
	if ps.Config == nil {
		return fmt.Errorf("%w: config is nil", ErrInvalidPairSpec)
	}
	if err := ps.Config.Validate(); err != nil {
		return fmt.Errorf("%w: config invalid: %v", ErrInvalidPairSpec, err)
	}
	if ps.DeclaredChange == "" {
		return fmt.Errorf("%w: declared_change is empty", ErrInvalidPairSpec)
	}
	if ps.MutationType == "" {
		return fmt.Errorf("%w: mutation_type is empty", ErrInvalidPairSpec)
	}
	return nil
}

// ─── Run result ───────────────────────────────────────────────────────────────

// RunResult is the output of one run (baseline or variant) of the evaluator.
type RunResult struct {
	ID           uuid.UUID          `json:"id"`
	PairID       uuid.UUID          `json:"pair_id"`
	RunType      RunType            `json:"run_type"`
	ControlHash  string             `json:"control_hash"`
	ManifestHash string             `json:"manifest_hash"`
	AlertCount   int                `json:"alert_count"`
	AlertSummary map[aml.RuleID]int `json:"alert_summary"`
	RawAlerts    []aml.Alert        `json:"raw_alerts,omitempty"`
	StartedAt    time.Time          `json:"started_at"`
	CompletedAt  time.Time          `json:"completed_at"`
}

// RunType distinguishes a baseline from a variant run.
type RunType string

const (
	RunTypeBaseline RunType = "baseline"
	RunTypeVariant  RunType = "variant"
)

// ─── Outcome ──────────────────────────────────────────────────────────────────

// Outcome is the diff between the baseline and variant runs.
// It is computed by ComparePair and persisted in experiment_outcome.
type Outcome struct {
	ID                  uuid.UUID    `json:"id"`
	PairID              uuid.UUID    `json:"pair_id"`
	BaselineRunID       uuid.UUID    `json:"baseline_run_id"`
	VariantRunID        uuid.UUID    `json:"variant_run_id"`
	EvasionDelta        int          `json:"evasion_delta"`          // baseline_alerts - variant_alerts; positive = evasion
	NewAlertRuleIDs     []aml.RuleID `json:"new_alert_rule_ids"`     // in variant but not baseline
	DroppedAlertRuleIDs []aml.RuleID `json:"dropped_alert_rule_ids"` // in baseline but not variant
	OutcomeUnchanged    bool         `json:"outcome_unchanged"`      // true if diff is empty
	CreatedAt           time.Time    `json:"created_at"`
}

// ─── Pair record ─────────────────────────────────────────────────────────────

// Pair is the persisted top-level record for one experiment pair.
type Pair struct {
	ID             uuid.UUID      `json:"id"`
	Name           string         `json:"name"`
	CohortID       uuid.UUID      `json:"cohort_id"`
	ControlHash    string         `json:"control_hash"`
	ManifestHash   string         `json:"manifest_hash"`
	Seed           int64          `json:"seed"`
	DeclaredChange string         `json:"declared_change"`
	MutationType   MutationType   `json:"mutation_type"`
	MutationSpec   map[string]any `json:"mutation_spec"` // any: open-schema blob per AGENTS.md rule
	Status         Status         `json:"status"`
	ErrorMsg       string         `json:"error_msg,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// ─── Sentinel errors ──────────────────────────────────────────────────────────

var (
	// ErrInvalidPairSpec is returned when a PairSpec fails validation.
	ErrInvalidPairSpec = errors.New("invalid pair spec")

	// ErrIncomparablePair is returned when two runs cannot be compared
	// because their control_hash or manifest_hash differ.
	ErrIncomparablePair = errors.New("runs are not comparable: control or manifest hash mismatch")

	// ErrPairAlreadyRun is returned when an attempt is made to re-execute
	// an already-completed or failed pair.
	ErrPairAlreadyRun = errors.New("pair has already been executed")
)
