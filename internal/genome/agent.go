package genome

import (
	"sort"

	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

// Agent provides batch genome encoding, pairwise novelty computation, and
// diversity-aware candidate selection. It is the "genome agent" of the adaptive
// discovery loop: given a pool of proposed scenarios, it quantifies how novel
// each one is relative to the others and selects a maximally-diverse subset.
//
// This backs the novelty term N(x) in the adaptive score
// S(x) = wc·C + wn·N + wi·I + wv·V.
type Agent struct {
	// minNovelty is the novelty floor below which a candidate is considered a
	// near-duplicate of another and may be dropped. Zero disables filtering.
	minNovelty float64
}

// AgentOption configures an Agent.
type AgentOption func(*Agent)

// WithMinNovelty sets the novelty floor for duplicate filtering.
// A candidate whose maximum similarity to any other candidate is above
// (1 - minNovelty) is considered a near-duplicate.
func WithMinNovelty(n float64) AgentOption {
	return func(a *Agent) { a.minNovelty = n }
}

// NewAgent constructs a genome Agent with the given options.
func NewAgent(opts ...AgentOption) *Agent {
	a := &Agent{}
	for _, o := range opts {
		o(a)
	}
	return a
}

// ScoredCandidate pairs a proposal with its encoded genome and novelty score.
type ScoredCandidate struct {
	Proposal scenario.Proposal
	Genome   Genome
	// Novelty is the minimum Euclidean distance from this candidate's genome to
	// any other candidate's genome, in [0,1]. Higher = more different.
	Novelty float64
	// NearestID is the ID of the closest other candidate (empty if only one).
	NearestID string
}

// EncodeBatch encodes every proposal into its genome vector.
// The returned slice is parallel to the input; the input is not modified.
func (a *Agent) EncodeBatch(proposals []scenario.Proposal) []Genome {
	out := make([]Genome, len(proposals))
	for i := range proposals {
		Encode(&out[i], proposals[i])
	}
	return out
}

// DistanceMatrix computes the full N×N pairwise Euclidean distance matrix.
// The diagonal is 0.0 and the matrix is symmetric.
func (a *Agent) DistanceMatrix(proposals []scenario.Proposal) [][]float64 {
	genomes := a.EncodeBatch(proposals)
	n := len(genomes)
	m := make([][]float64, n)
	for i := range m {
		m[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			d := Distance(genomes[i], genomes[j])
			m[i][j] = d
			m[j][i] = d
		}
	}
	return m
}

// ScoreBatch encodes all proposals and computes each candidate's novelty as the
// minimum distance to any other candidate. Results are returned in the same
// order as the input.
func (a *Agent) ScoreBatch(proposals []scenario.Proposal) []ScoredCandidate {
	genomes := a.EncodeBatch(proposals)
	n := len(genomes)

	out := make([]ScoredCandidate, n)
	for i := range proposals {
		out[i] = ScoredCandidate{Proposal: proposals[i], Genome: genomes[i]}
		// Novelty = min distance to any other candidate. A single candidate is
		// considered maximally novel (no peers to be similar to).
		if n <= 1 {
			out[i].Novelty = 1.0
			continue
		}
		best := 1.0
		nearest := ""
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			d := Distance(genomes[i], genomes[j])
			if d < best {
				best = d
				nearest = proposals[j].ID.String()
			}
		}
		out[i].Novelty = best
		out[i].NearestID = nearest
	}
	return out
}

// RankByNovelty returns candidates sorted by descending novelty (most novel first).
// Ties are broken by proposal ID for determinism.
func (a *Agent) RankByNovelty(proposals []scenario.Proposal) []ScoredCandidate {
	scored := a.ScoreBatch(proposals)
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Novelty != scored[j].Novelty {
			return scored[i].Novelty > scored[j].Novelty
		}
		return scored[i].Proposal.ID.String() < scored[j].Proposal.ID.String()
	})
	return scored
}

// SelectDiverse greedily selects up to k candidates that are maximally diverse.
//
// Algorithm (farthest-point / max-min diversity):
//  1. Seed with the highest-novelty candidate.
//  2. Repeatedly add the candidate whose minimum distance to the already-selected
//     set is largest.
//
// This produces a spread-out subset that maximises coverage of genome space,
// which raises the expected information gain of an experiment batch.
// The returned slice is in selection order.
func (a *Agent) SelectDiverse(proposals []scenario.Proposal, k int) []ScoredCandidate {
	if k <= 0 || len(proposals) == 0 {
		return nil
	}
	if k > len(proposals) {
		k = len(proposals)
	}

	genomes := a.EncodeBatch(proposals)
	scored := a.ScoreBatch(proposals)

	// Step 1: seed with the most novel candidate.
	seedIdx := 0
	for i := 1; i < len(scored); i++ {
		if scored[i].Novelty > scored[seedIdx].Novelty {
			seedIdx = i
		}
	}

	selected := make([]int, 0, k)
	chosen := make([]bool, len(proposals))
	selected = append(selected, seedIdx)
	chosen[seedIdx] = true

	// Step 2: greedily add the farthest point from the selected set.
	for len(selected) < k {
		bestIdx := -1
		bestMinDist := -1.0
		for i := range proposals {
			if chosen[i] {
				continue
			}
			// min distance from i to any already-selected candidate
			minDist := 2.0
			for _, s := range selected {
				d := Distance(genomes[i], genomes[s])
				if d < minDist {
					minDist = d
				}
			}
			if minDist > bestMinDist ||
				(minDist == bestMinDist && (bestIdx == -1 || proposals[i].ID.String() < proposals[bestIdx].ID.String())) {
				bestMinDist = minDist
				bestIdx = i
			}
		}
		if bestIdx == -1 {
			break
		}
		selected = append(selected, bestIdx)
		chosen[bestIdx] = true
	}

	out := make([]ScoredCandidate, 0, len(selected))
	for _, idx := range selected {
		out = append(out, scored[idx])
	}
	return out
}

// FilterDuplicates removes candidates whose novelty falls below the agent's
// configured minNovelty threshold (default 0 disables filtering).
// Order of the surviving candidates is preserved.
func (a *Agent) FilterDuplicates(proposals []scenario.Proposal) []scenario.Proposal {
	if a.minNovelty <= 0 {
		return proposals
	}
	scored := a.ScoreBatch(proposals)
	out := make([]scenario.Proposal, 0, len(proposals))
	for _, s := range scored {
		if s.Novelty >= a.minNovelty {
			out = append(out, s.Proposal)
		}
	}
	return out
}

// CoverageSpread returns the mean pairwise distance of the given candidates,
// a scalar measure of how much genome space the set spans. In [0,1].
// A single candidate has spread 0.
func (a *Agent) CoverageSpread(proposals []scenario.Proposal) float64 {
	n := len(proposals)
	if n < 2 {
		return 0
	}
	genomes := a.EncodeBatch(proposals)
	var sum float64
	var pairs int
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			sum += Distance(genomes[i], genomes[j])
			pairs++
		}
	}
	if pairs == 0 {
		return 0
	}
	return sum / float64(pairs)
}
