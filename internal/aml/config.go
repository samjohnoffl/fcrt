// Package aml — config.go
//
// ControlConfig is the versioned AML rule configuration that governs
// one experiment. It is content-addressed: the ConfigHash is SHA-256 of
// the canonical JSON encoding, so any change to thresholds or rule set
// produces a different hash and therefore an incomparable experiment.
//
// Design rule: ControlConfig is immutable after construction. Callers
// should treat it as a value type; mutating fields after hashing will
// silently invalidate the hash.
package aml

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// ControlVersion is the semantic version of the AML control config schema.
// Bump this when the schema of ControlConfig itself changes (not just thresholds).
const ControlVersion = "v0.1.0"

// StructuringConfig holds thresholds for the structuring detection rule.
// Structuring: multiple transactions just below a reporting threshold to
// the same counterparty within a time window.
type StructuringConfig struct {
	// ReportingThresholdCents is the single-transaction amount (in cents) that
	// would require reporting. Transactions below this are suspect.
	ReportingThresholdCents int64 `json:"reporting_threshold_cents"`
	// WindowHours is the look-back window within which sub-threshold transactions
	// to the same counterparty are aggregated.
	WindowHours int `json:"window_hours"`
	// MinTransactions is the minimum number of sub-threshold transactions to the
	// same counterparty within the window to trigger an alert.
	MinTransactions int `json:"min_transactions"`
}

// LayeringConfig holds thresholds for the layering detection rule.
// Layering: funds moved through ≥N intermediary accounts within a time window.
type LayeringConfig struct {
	// MinHops is the minimum number of account-to-account hops to qualify.
	MinHops int `json:"min_hops"`
	// WindowHours is the look-back window for tracing a chain.
	WindowHours int `json:"window_hours"`
}

// RapidMovementConfig holds thresholds for the rapid movement detection rule.
// Rapid movement: a large amount credited to an account and debited within a
// short time window (round-tripping / pass-through behaviour).
type RapidMovementConfig struct {
	// MinAmountCents is the minimum credit amount (cents) to trigger monitoring.
	MinAmountCents int64 `json:"min_amount_cents"`
	// WindowMinutes is the time window (minutes) between credit and subsequent debit.
	WindowMinutes int `json:"window_minutes"`
	// DebitRatioPct is the minimum percentage of the credited amount that must be
	// debited within the window to fire an alert. Range 0–100.
	DebitRatioPct int `json:"debit_ratio_pct"`
}

// EnabledRules lists which rule IDs are active in this config.
// Evaluator skips any rule whose ID is not in this list.
type EnabledRules []RuleID

// ControlConfig is the complete, versioned AML rule configuration for one
// experiment pair. Both the baseline and variant MUST use the same
// ControlConfig (verified via ConfigHash equality).
type ControlConfig struct {
	// ControlVersion is the schema version of this struct.
	ControlVersion string `json:"control_version"`
	// EnabledRules lists which rules are active.
	EnabledRules EnabledRules `json:"enabled_rules"`
	// Structuring holds structuring rule parameters.
	Structuring StructuringConfig `json:"structuring"`
	// Layering holds layering rule parameters.
	Layering LayeringConfig `json:"layering"`
	// RapidMovement holds rapid-movement rule parameters.
	RapidMovement RapidMovementConfig `json:"rapid_movement"`
	// ConfigHash is SHA-256 of the canonical JSON (with ConfigHash="").
	// Populated by NewControlConfig; do not set manually.
	ConfigHash string `json:"config_hash"`
}

// DefaultControlConfig returns a standard ControlConfig that reflects
// representative real-world AML thresholds. All three rules are enabled.
func DefaultControlConfig() *ControlConfig {
	c := &ControlConfig{
		ControlVersion: ControlVersion,
		EnabledRules:   EnabledRules{RuleStructuring, RuleLayering, RuleRapidMovement},
		Structuring: StructuringConfig{
			ReportingThresholdCents: 1_000_000, // $10,000.00
			WindowHours:             24,
			MinTransactions:         3,
		},
		Layering: LayeringConfig{
			MinHops:     3,
			WindowHours: 72,
		},
		RapidMovement: RapidMovementConfig{
			MinAmountCents: 500_000, // $5,000.00
			WindowMinutes:  60,
			DebitRatioPct:  80,
		},
	}
	if err := c.computeHash(); err != nil {
		// Static config: this should never fail.
		panic(fmt.Sprintf("aml: failed to hash default config: %v", err))
	}
	return c
}

// NewControlConfig constructs a ControlConfig from the given parameters,
// computes its content hash, and returns it.
func NewControlConfig(
	enabled EnabledRules,
	structuring StructuringConfig,
	layering LayeringConfig,
	rapidMovement RapidMovementConfig,
) (*ControlConfig, error) {
	c := &ControlConfig{
		ControlVersion: ControlVersion,
		EnabledRules:   enabled,
		Structuring:    structuring,
		Layering:       layering,
		RapidMovement:  rapidMovement,
	}
	if err := c.computeHash(); err != nil {
		return nil, err
	}
	return c, nil
}

// computeHash sets ConfigHash to SHA-256 of the canonical JSON of the config
// with ConfigHash temporarily cleared to the empty string.
// This gives a stable, content-addressable identity.
func (c *ControlConfig) computeHash() error {
	saved := c.ConfigHash
	c.ConfigHash = ""
	b, err := json.Marshal(c)
	c.ConfigHash = saved // restore before checking error
	if err != nil {
		return fmt.Errorf("aml: marshal config for hashing: %w", err)
	}
	sum := sha256.Sum256(b)
	c.ConfigHash = fmt.Sprintf("%x", sum)
	return nil
}

// IsRuleEnabled reports whether the given rule ID is active in this config.
func (c *ControlConfig) IsRuleEnabled(id RuleID) bool {
	for _, r := range c.EnabledRules {
		if r == id {
			return true
		}
	}
	return false
}

// Validate returns an error if the config contains invalid parameter values.
func (c *ControlConfig) Validate() error {
	if c.ControlVersion == "" {
		return fmt.Errorf("aml: control_version is empty")
	}
	if len(c.EnabledRules) == 0 {
		return fmt.Errorf("aml: enabled_rules is empty")
	}
	if c.Structuring.ReportingThresholdCents <= 0 {
		return fmt.Errorf("aml: structuring.reporting_threshold_cents must be > 0")
	}
	if c.Structuring.WindowHours <= 0 {
		return fmt.Errorf("aml: structuring.window_hours must be > 0")
	}
	if c.Structuring.MinTransactions <= 1 {
		return fmt.Errorf("aml: structuring.min_transactions must be > 1")
	}
	if c.Layering.MinHops <= 1 {
		return fmt.Errorf("aml: layering.min_hops must be > 1")
	}
	if c.RapidMovement.DebitRatioPct < 0 || c.RapidMovement.DebitRatioPct > 100 {
		return fmt.Errorf("aml: rapid_movement.debit_ratio_pct must be in [0,100]")
	}
	if c.ConfigHash == "" {
		return fmt.Errorf("aml: config_hash is empty (use NewControlConfig or DefaultControlConfig)")
	}
	return nil
}
