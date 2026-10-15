// Package scenario — selector.go
//
// Selector implements the adaptive candidate-selection algorithm.
//
// Algorithm overview (per SelectionState):
//
//  1. Coverage score — inversely proportional to how many completed experiments
//     already use the same mutation_type. A never-tried type scores 1.0.
//
//  2. Novelty score — 1 − max_jaccard(candidate.tags, existing_pool.tags).
//     Jaccard similarity of tag sets measures semantic overlap. A candidate
//     with a completely new tag set scores 1.0.
//
//  3. Validity score — 1.0 if schema validation passed, 0.0 otherwise.
//     Partially invalid candidates are rejected before scoring, so in
//     practice this is always 1.0 for candidates that reach the scorer.
//
//  4. EIV (Expected Information Value) — taken directly from the model's
//     eiv_confidence field (clamped to [0,1]) if present; otherwise 0.5.
//
// Deduplication: candidates whose FingerprintKey matches an existing key in
// SelectionState.SeenFingerprints are rejected with RejectDuplicate before scoring.
//
// Output: candidates sorted descending by composite score; top N are returned.
package scenario

import (
	"sort"
	"time"
)

// ─── Selection state ─────────────────────────────────────────────────────────

// SelectionState captures the current coverage landscape against which new
// candidates are scored. It should be rebuilt from the experiment_pair table
// before each selection run.
type SelectionState struct {
	// MutationTypeCounts maps each MutationType to the number of completed
	// experiments that used it. Zero for never-tried types.
	MutationTypeCounts map[MutationType]int

	// TotalExperiments is the total count of completed experiment pairs.
	// Used to normalise coverage scores.
	TotalExperiments int

	// PoolTagSets is the collection of tag sets from candidates already in the
	// current selection pool. Used for novelty scoring.
	// Each element is the tag set of one previously selected candidate.
	PoolTagSets [][]string

	// SeenFingerprints is the set of FingerprintKey values from candidates
	// already selected in this session. Populated incrementally.
	SeenFingerprints map[string]bool
}

// NewSelectionState returns a zero-value SelectionState ready for use.
func NewSelectionState() SelectionState {
	return SelectionState{
		MutationTypeCounts: make(map[MutationType]int),
		SeenFingerprints:   make(map[string]bool),
	}
}

// ─── Selector ─────────────────────────────────────────────────────────────────

// Selector scores and ranks candidates using the adaptive algorithm.
type Selector struct {
	weights SelectionWeights
}

// NewSelector creates a Selector with the given weights.
// Returns an error if the weights fail validation.
func NewSelector(weights SelectionWeights) (*Selector, error) {
	if err := weights.Validate(); err != nil {
		return nil, err
	}
	return &Selector{weights: weights}, nil
}

// Select scores all accepted candidates from a GenerationResult, deduplicates
// them against state.SeenFingerprints, ranks by composite score, and returns
// the top maxSelect candidates.
//
// The state is updated in place: SeenFingerprints and PoolTagSets are extended
// with each accepted candidate.
func (s *Selector) Select(
	result GenerationResult,
	state *SelectionState,
	maxSelect int,
) SelectionResult {
	var rejected []RejectionRecord
	// Copy Gemini validation rejections into our output.
	rejected = append(rejected, result.Rejected...)

	var scored []Candidate

	for i := range result.Accepted {
		c := result.Accepted[i]

		// Duplicate check.
		fp := c.FingerprintKey()
		if state.SeenFingerprints[fp] {
			rejected = append(rejected, RejectionRecord{
				CorrelationID: c.CorrelationID,
				Reason:        RejectDuplicate,
				Detail:        "fingerprint already seen: mutation_type=" + string(c.MutationType),
				RejectedAt:    time.Now(),
			})
			continue
		}

		// Score all four components.
		c.ScoreComponents.Coverage = s.coverageScore(c.MutationType, state)
		c.ScoreComponents.Novelty = s.noveltyScore(c.Tags, state)
		// Validity is already set to 1.0 by ParseAndValidate for accepted candidates.
		// EIV is already set from eiv_confidence by ParseAndValidate.

		c.Score = s.weights.Score(c.ScoreComponents)
		scored = append(scored, c)
	}

	// Sort descending by composite score.
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		// Tie-break: alphabetical by fingerprint for determinism.
		return scored[i].FingerprintKey() < scored[j].FingerprintKey()
	})

	// Take top maxSelect.
	selected := scored
	if len(selected) > maxSelect {
		// Candidates beyond maxSelect are rejected with budget reason.
		for _, c := range scored[maxSelect:] {
			rejected = append(rejected, RejectionRecord{
				CorrelationID: c.CorrelationID,
				Reason:        RejectBudgetExceeded,
				Detail:        "ranked below top-N cutoff",
				RejectedAt:    time.Now(),
			})
		}
		selected = scored[:maxSelect]
	}

	// Update state with selected candidates.
	for i := range selected {
		fp := selected[i].FingerprintKey()
		state.SeenFingerprints[fp] = true
		state.PoolTagSets = append(state.PoolTagSets, selected[i].Tags)
		state.MutationTypeCounts[selected[i].MutationType]++
		state.TotalExperiments++
	}

	return SelectionResult{
		Selected:       selected,
		Rejected:       rejected,
		WeightsVersion: s.weights.Version,
	}
}

// ─── Coverage scoring ─────────────────────────────────────────────────────────

// coverageScore returns a score in [0,1] that is high when the given
// mutation_type is under-represented in completed experiments.
//
// Formula:
//
//	If TotalExperiments == 0: return 1.0 (everything is novel).
//	typeFreq = MutationTypeCounts[mt] / TotalExperiments
//	score    = 1.0 − typeFreq
//
// This gives a score of 1.0 for a never-tried type, 0.0 for a type that
// accounts for 100% of past experiments.
func (s *Selector) coverageScore(mt MutationType, state *SelectionState) float64 {
	if state.TotalExperiments == 0 {
		return 1.0
	}
	freq := float64(state.MutationTypeCounts[mt]) / float64(state.TotalExperiments)
	return clamp01(1.0 - freq)
}

// ─── Novelty scoring ──────────────────────────────────────────────────────────

// noveltyScore returns 1 − max_jaccard(candidateTags, each poolTagSet).
//
// Jaccard similarity of two tag sets A and B:
//
//	J(A,B) = |A ∩ B| / |A ∪ B|
//
// A score of 1.0 means the candidate shares no tags with any existing pool
// member. A score of 0.0 means it is identical to an existing member.
//
// If the pool is empty, returns 1.0 (maximum novelty).
func (s *Selector) noveltyScore(candidateTags []string, state *SelectionState) float64 {
	if len(state.PoolTagSets) == 0 {
		return 1.0
	}
	maxJ := 0.0
	for _, poolTags := range state.PoolTagSets {
		j := jaccardSimilarity(candidateTags, poolTags)
		if j > maxJ {
			maxJ = j
		}
	}
	return clamp01(1.0 - maxJ)
}

// jaccardSimilarity computes |A ∩ B| / |A ∪ B| for two string slices.
// Returns 0.0 when both sets are empty (no information, not maximally similar).
func jaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0.0
	}
	setA := make(map[string]bool, len(a))
	for _, v := range a {
		setA[v] = true
	}
	setB := make(map[string]bool, len(b))
	for _, v := range b {
		setB[v] = true
	}

	intersection := 0
	for v := range setA {
		if setB[v] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}
