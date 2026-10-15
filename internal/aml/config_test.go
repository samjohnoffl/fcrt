// Package aml — config_test.go
// Unit tests for ControlConfig: hashing, validation, and immutability.
package aml

import (
	"encoding/json"
	"testing"
)

// ─── DefaultControlConfig ─────────────────────────────────────────────────────

func TestDefaultControlConfig_HasHash(t *testing.T) {
	cfg := DefaultControlConfig()
	if cfg.ConfigHash == "" {
		t.Fatal("DefaultControlConfig has empty ConfigHash")
	}
}

func TestDefaultControlConfig_HashIsDeterministic(t *testing.T) {
	h1 := DefaultControlConfig().ConfigHash
	h2 := DefaultControlConfig().ConfigHash
	if h1 != h2 {
		t.Errorf("hash not deterministic: %s vs %s", h1, h2)
	}
}

func TestDefaultControlConfig_Validates(t *testing.T) {
	if err := DefaultControlConfig().Validate(); err != nil {
		t.Errorf("DefaultControlConfig failed validation: %v", err)
	}
}

// ─── NewControlConfig ─────────────────────────────────────────────────────────

func TestNewControlConfig_HashChangesWithThreshold(t *testing.T) {
	cfg1, _ := NewControlConfig(
		EnabledRules{RuleStructuring},
		StructuringConfig{ReportingThresholdCents: 1_000_000, WindowHours: 24, MinTransactions: 3},
		LayeringConfig{MinHops: 3, WindowHours: 72},
		RapidMovementConfig{MinAmountCents: 500_000, WindowMinutes: 60, DebitRatioPct: 80},
	)
	cfg2, _ := NewControlConfig(
		EnabledRules{RuleStructuring},
		StructuringConfig{ReportingThresholdCents: 2_000_000, WindowHours: 24, MinTransactions: 3}, // different
		LayeringConfig{MinHops: 3, WindowHours: 72},
		RapidMovementConfig{MinAmountCents: 500_000, WindowMinutes: 60, DebitRatioPct: 80},
	)
	if cfg1.ConfigHash == cfg2.ConfigHash {
		t.Error("different thresholds produced the same ConfigHash")
	}
}

func TestNewControlConfig_HashChangesWithEnabledRules(t *testing.T) {
	base := StructuringConfig{ReportingThresholdCents: 1_000_000, WindowHours: 24, MinTransactions: 3}
	lay := LayeringConfig{MinHops: 3, WindowHours: 72}
	rm := RapidMovementConfig{MinAmountCents: 500_000, WindowMinutes: 60, DebitRatioPct: 80}

	cfg1, _ := NewControlConfig(EnabledRules{RuleStructuring}, base, lay, rm)
	cfg2, _ := NewControlConfig(EnabledRules{RuleStructuring, RuleLayering}, base, lay, rm)

	if cfg1.ConfigHash == cfg2.ConfigHash {
		t.Error("different enabled_rules produced the same ConfigHash")
	}
}

func TestNewControlConfig_HashExcludesItself(t *testing.T) {
	// The hash must be computed with config_hash="" so that serialising
	// the struct back to JSON with the hash included does NOT change the hash.
	cfg, err := NewControlConfig(
		EnabledRules{RuleStructuring},
		StructuringConfig{ReportingThresholdCents: 1_000_000, WindowHours: 24, MinTransactions: 3},
		LayeringConfig{MinHops: 3, WindowHours: 72},
		RapidMovementConfig{MinAmountCents: 500_000, WindowMinutes: 60, DebitRatioPct: 80},
	)
	if err != nil {
		t.Fatalf("NewControlConfig: %v", err)
	}
	// Serialise and deserialise.
	b, _ := json.Marshal(cfg)
	var cfg2 ControlConfig
	json.Unmarshal(b, &cfg2)
	// Re-hash cfg2 from scratch by zeroing the hash.
	cfg2.ConfigHash = ""
	cfg2.computeHash() // unexported but in same package

	if cfg.ConfigHash != cfg2.ConfigHash {
		t.Errorf("hash changed after roundtrip: %s vs %s", cfg.ConfigHash, cfg2.ConfigHash)
	}
}

// ─── Validation ───────────────────────────────────────────────────────────────

func TestControlConfig_Validate_EmptyEnabledRules(t *testing.T) {
	cfg := DefaultControlConfig()
	cfg.EnabledRules = nil
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty enabled_rules")
	}
}

func TestControlConfig_Validate_ZeroThreshold(t *testing.T) {
	cfg := DefaultControlConfig()
	cfg.Structuring.ReportingThresholdCents = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for zero reporting threshold")
	}
}

func TestControlConfig_Validate_BadDebitRatio(t *testing.T) {
	cfg := DefaultControlConfig()
	cfg.RapidMovement.DebitRatioPct = 150
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for debit_ratio_pct > 100")
	}
}

func TestControlConfig_Validate_EmptyHash(t *testing.T) {
	cfg := DefaultControlConfig()
	cfg.ConfigHash = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty config_hash")
	}
}

// ─── IsRuleEnabled ────────────────────────────────────────────────────────────

func TestControlConfig_IsRuleEnabled(t *testing.T) {
	cfg, _ := NewControlConfig(
		EnabledRules{RuleStructuring, RuleLayering},
		StructuringConfig{ReportingThresholdCents: 1_000_000, WindowHours: 24, MinTransactions: 3},
		LayeringConfig{MinHops: 3, WindowHours: 72},
		RapidMovementConfig{MinAmountCents: 500_000, WindowMinutes: 60, DebitRatioPct: 80},
	)
	if !cfg.IsRuleEnabled(RuleStructuring) {
		t.Error("structuring should be enabled")
	}
	if !cfg.IsRuleEnabled(RuleLayering) {
		t.Error("layering should be enabled")
	}
	if cfg.IsRuleEnabled(RuleRapidMovement) {
		t.Error("rapid_movement should not be enabled")
	}
}
