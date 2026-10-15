// Package scenario — weights.go
//
// SelectionWeights defines the versioned, documented coefficients for the
// four scoring components. Weights must sum to 1.0; this is enforced at
// construction time.
//
// Version history:
//
//	v0.1.0 — initial weights: Coverage 0.30, Novelty 0.25, Validity 0.25, EIV 0.20.
//	         Rationale: coverage and novelty drive diversity first; EIV is
//	         down-weighted because model self-reported confidence is uncalibrated.
package scenario

import (
	"fmt"
	"math"
)

// WeightsVersion is the current schema version of SelectionWeights.
// Bump whenever the meaning or default values of any component change.
const WeightsVersion = "v0.1.0"

// SelectionWeights holds the four scoring coefficients.
// All values must be in [0, 1] and must sum to 1.0 (±1e-6 tolerance).
//
// Component definitions:
//
//	Coverage  — proportion of the score contributed by mutation-type coverage.
//	            High coverage weight encourages trying under-represented mutation types.
//	Novelty   — proportion from tag-set dissimilarity to the existing pool.
//	            High novelty weight discourages near-duplicate candidates.
//	Validity  — proportion from schema + spec validation.
//	            Weight is intentionally non-zero so that partially valid candidates
//	            score lower than fully valid ones (reserved for future soft validation).
//	EIV       — proportion from the model's self-reported expected information value.
//	            Currently down-weighted because Gemini's eiv_confidence is uncalibrated.
type SelectionWeights struct {
	// Version identifies this weight set for reproducibility.
	Version string `json:"version"`

	// Coverage weight. Default 0.30.
	Coverage float64 `json:"coverage"`

	// Novelty weight. Default 0.25.
	Novelty float64 `json:"novelty"`

	// Validity weight. Default 0.25.
	Validity float64 `json:"validity"`

	// EIV (Expected Information Value) weight. Default 0.20.
	EIV float64 `json:"eiv"`
}

// DefaultWeights returns the current production weights (v0.1.0).
// These are the weights used when no custom configuration is provided.
func DefaultWeights() SelectionWeights {
	return SelectionWeights{
		Version:  WeightsVersion,
		Coverage: 0.30,
		Novelty:  0.25,
		Validity: 0.25,
		EIV:      0.20,
	}
}

// Validate returns an error if the weights are invalid.
func (w SelectionWeights) Validate() error {
	if w.Version == "" {
		return fmt.Errorf("weights: Version is empty")
	}
	for name, v := range map[string]float64{
		"Coverage": w.Coverage,
		"Novelty":  w.Novelty,
		"Validity": w.Validity,
		"EIV":      w.EIV,
	} {
		if v < 0 || v > 1 {
			return fmt.Errorf("weights: %s=%v must be in [0,1]", name, v)
		}
	}
	total := w.Coverage + w.Novelty + w.Validity + w.EIV
	if math.Abs(total-1.0) > 1e-6 {
		return fmt.Errorf("weights: sum=%.6f must equal 1.0 (±1e-6)", total)
	}
	return nil
}

// Score computes the weighted composite score for a set of components.
// All inputs are assumed to be in [0,1].
func (w SelectionWeights) Score(sc ScoreComponents) float64 {
	return w.Coverage*sc.Coverage +
		w.Novelty*sc.Novelty +
		w.Validity*sc.Validity +
		w.EIV*sc.EIV
}
