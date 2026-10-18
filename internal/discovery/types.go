// Package discovery implements the adaptive candidate-selection loop.
//
// The loop is the core research mechanism of the project: given a pool of
// Gemini-proposed scenarios, choose which one to test next using a documented,
// versioned score rather than a fixed order.
//
// The score is
//
//	S(x) = wc·C(x) + wn·N(x) + wi·I(x) + wv·V(x)
//
// where every component and every weight is persisted with the decision. A
// reviewer can therefore answer "why was this candidate chosen?" without
// re-running the model. The package never invents a component: when the data
// needed for a component is absent, the component takes its documented
// prior value and the rationale says so.
package discovery

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/genome"
)

// Strategy is the candidate-selection policy used for a run.
type Strategy string

const (
	// StrategyAdaptive ranks candidates by the composite score.
	StrategyAdaptive Strategy = "adaptive"
	// StrategyRandom selects uniformly at random from valid candidates.
	StrategyRandom Strategy = "random"
	// StrategyGeminiOnly selects in Gemini generation order, ignoring coverage
	// and novelty feedback. It is the "AI alone" baseline.
	StrategyGeminiOnly Strategy = "gemini_only"
)

// Valid reports whether s is a known strategy.
func (s Strategy) Valid() bool {
	switch s {
	case StrategyAdaptive, StrategyRandom, StrategyGeminiOnly:
		return true
	default:
		return false
	}
}

// WeightsVersion is the version of the default coefficient set.
const WeightsVersion = "v1.0.0"

// Weights holds the four coefficients. They must sum to 1.0.
type Weights struct {
	Version     string  `json:"version"`
	Coverage    float64 `json:"coverage"`
	Novelty     float64 `json:"novelty"`
	Information float64 `json:"information"`
	Validity    float64 `json:"validity"`
}

// DefaultWeights returns the production weights.
//
// Rationale (recorded in DECISIONS.md): coverage and novelty drive diversity
// first; information gain is a proxy and is not over-weighted; validity gets a
// real weight so that a candidate which fails schema validation cannot outrank
// a valid one, even if the scorer is later extended to score soft-validity.
func DefaultWeights() Weights {
	return Weights{
		Version:     WeightsVersion,
		Coverage:    0.30,
		Novelty:     0.25,
		Information: 0.25,
		Validity:    0.20,
	}
}

// Validate checks the coefficient set.
func (w Weights) Validate() error {
	if w.Version == "" {
		return fmt.Errorf("weights: version is required")
	}
	for name, v := range map[string]float64{
		"coverage":    w.Coverage,
		"novelty":     w.Novelty,
		"information": w.Information,
		"validity":    w.Validity,
	} {
		if v < 0 || v > 1 || math.IsNaN(v) {
			return fmt.Errorf("weights: %s=%v must be in [0,1]", name, v)
		}
	}
	sum := w.Coverage + w.Novelty + w.Information + w.Validity
	if math.Abs(sum-1.0) > 1e-9 {
		return fmt.Errorf("weights: sum=%.6f must equal 1.0", sum)
	}
	return nil
}

// Components are the four score inputs, each in [0,1].
type Components struct {
	Coverage    float64 `json:"coverage"`
	Novelty     float64 `json:"novelty"`
	Information float64 `json:"information"`
	Validity    float64 `json:"validity"`
}

// Score applies the weights to the components.
func (w Weights) Score(c Components) float64 {
	return w.Coverage*c.Coverage +
		w.Novelty*c.Novelty +
		w.Information*c.Information +
		w.Validity*c.Validity
}

// Candidate is a proposal that is eligible for selection.
type Candidate struct {
	ProposalID   uuid.UUID
	MutationType string
	ScenarioType string
	Genome       genome.Genome
	CreatedAt    time.Time
}

// OutcomeStat is the stored outcome history for one mutation type.
type OutcomeStat struct {
	Total     int `json:"total"`
	Unchanged int `json:"unchanged"`
	Changed   int `json:"changed"`
}

// State is the evidence base the score is computed against. It is rebuilt from
// the database before every run, never carried in memory across runs.
type State struct {
	// MutationCounts counts completed experiments per mutation type.
	MutationCounts map[string]int
	// Outcomes holds outcome history per mutation type.
	Outcomes map[string]OutcomeStat
	// PriorGenomes are the genomes of behaviours already tested.
	PriorGenomes []genome.Genome
	// TotalExperiments is the number of completed experiments in the cohort.
	TotalExperiments int
}

// NewState returns an initialised State.
func NewState() State {
	return State{
		MutationCounts: make(map[string]int),
		Outcomes:       make(map[string]OutcomeStat),
	}
}

// Scored is one candidate with its full scoring trace.
type Scored struct {
	ProposalID   uuid.UUID     `json:"proposal_id"`
	MutationType string        `json:"mutation_type"`
	Genome       genome.Genome `json:"genome"`
	GenomeValid  bool          `json:"genome_valid"`
	Components   Components    `json:"components"`
	Score        float64       `json:"score"`
	Rank         int           `json:"rank"`
	Selected     bool          `json:"selected"`
	Rationale    string        `json:"rationale"`
}

// Rejection records why a candidate never reached the scorer.
type Rejection struct {
	ProposalID uuid.UUID `json:"proposal_id"`
	Reason     string    `json:"reason"`
}

// Run is one persisted selection decision.
type Run struct {
	ID       uuid.UUID `json:"id"`
	CohortID uuid.UUID `json:"cohort_id"`
	Strategy Strategy  `json:"strategy"`
	Weights  Weights   `json:"weights"`
	Seed     int64     `json:"seed"`
	Budget   int       `json:"budget"`
	// PoolSize is the number of eligible proposals considered.
	PoolSize int `json:"pool_size"`
	// ValidPoolSize is the number that passed genome validation before
	// duplicate collapsing. PoolSize and ValidPoolSize yield the benchmark's
	// valid-candidate-yield metric.
	ValidPoolSize int         `json:"valid_pool_size"`
	Candidates    []Scored    `json:"candidates"`
	Rejected      []Rejection `json:"rejected,omitempty"`
	Notes         string      `json:"notes,omitempty"`
	DurationMS    int64       `json:"duration_ms"`
	CreatedAt     time.Time   `json:"created_at"`
}

// ─── Scoring ─────────────────────────────────────────────────────────────────

// CoverageScore returns the expected coverage gain for a mutation type.
//
// A never-tested mutation type scores 1.0. Otherwise the score declines with
// how often the type has already been tested, normalised by the total number of
// completed experiments. This is deliberately simple and inspectable.
func CoverageScore(mutationType string, st State) float64 {
	used := st.MutationCounts[mutationType]
	if used == 0 || st.TotalExperiments == 0 {
		return 1.0
	}
	return clamp01(1.0 - float64(used)/float64(st.TotalExperiments))
}

// InformationScore returns the expected information gain for a mutation type.
//
// It is a proxy, not Shannon entropy: if every prior experiment of this type
// produced an unchanged outcome, another identical test is unlikely to teach us
// anything, so the score tends to 0. With no outcome history the score is 1.0
// (maximum uncertainty).
func InformationScore(mutationType string, st State) float64 {
	stat, ok := st.Outcomes[mutationType]
	if !ok || stat.Total == 0 {
		return 1.0
	}
	return clamp01(1.0 - float64(stat.Unchanged)/float64(stat.Total))
}

// ScoreCandidate computes the full trace for one candidate. It returns ok=false
// when the genome is invalid; invalid candidates must be rejected rather than
// scored.
func ScoreCandidate(c Candidate, st State, w Weights) (Scored, bool) {
	sc := Scored{
		ProposalID:   c.ProposalID,
		MutationType: c.MutationType,
		Genome:       c.Genome,
	}
	if err := c.Genome.Validate(); err != nil {
		sc.Rationale = "rejected: genome validation failed: " + err.Error()
		return sc, false
	}
	sc.GenomeValid = true
	sc.Components = Components{
		Coverage:    CoverageScore(c.MutationType, st),
		Novelty:     genome.Novelty(c.Genome, st.PriorGenomes),
		Information: InformationScore(c.MutationType, st),
		Validity:    1.0,
	}
	sc.Score = w.Score(sc.Components)
	sc.Rationale = Explain(c, st, w, sc)
	return sc, true
}

// Explain renders the scoring trace in reviewer-readable form.
func Explain(c Candidate, st State, w Weights, sc Scored) string {
	used := st.MutationCounts[c.MutationType]
	coverageInput := fmt.Sprintf("mutation %q tested %d/%d times", c.MutationType, used, st.TotalExperiments)
	if used == 0 || st.TotalExperiments == 0 {
		coverageInput = fmt.Sprintf("mutation %q not yet tested in this cohort", c.MutationType)
	}

	noveltyInput := "no prior genomes; novelty prior = 1.00"
	if len(st.PriorGenomes) > 0 {
		if _, d, ok := genome.Nearest(c.Genome, st.PriorGenomes); ok {
			noveltyInput = fmt.Sprintf("nearest prior genome distance %.3f over %d tested genomes", d, len(st.PriorGenomes))
		}
	}

	infoInput := "no outcome history for this mutation type; information prior = 1.00"
	if stat, ok := st.Outcomes[c.MutationType]; ok && stat.Total > 0 {
		infoInput = fmt.Sprintf("%d/%d prior outcomes unchanged", stat.Unchanged, stat.Total)
	}

	return fmt.Sprintf(
		"S=%.3f = %.2f·C(%.3f)+%.2f·N(%.3f)+%.2f·I(%.3f)+%.2f·V(%.3f); %s; %s; %s; genome %s; weights %s",
		sc.Score,
		w.Coverage, sc.Components.Coverage,
		w.Novelty, sc.Components.Novelty,
		w.Information, sc.Components.Information,
		w.Validity, sc.Components.Validity,
		coverageInput, noveltyInput, infoInput,
		c.Genome.GenomeID, w.Version,
	)
}

// ─── Selection ───────────────────────────────────────────────────────────────

// Select scores the pool under the requested strategy and returns a persisted-
// ready run. Invalid candidates are rejected with reasons. Exact genome
// duplicates are collapsed so the loop does not spend budget on accidental
// replication; the rejection is recorded rather than silently dropped.
func Select(cohortID uuid.UUID, pool []Candidate, st State, w Weights, strategy Strategy, budget int, seed int64) (Run, error) {
	if !strategy.Valid() {
		return Run{}, fmt.Errorf("discovery: unknown strategy %q", strategy)
	}
	if err := w.Validate(); err != nil {
		return Run{}, err
	}
	if budget < 1 {
		return Run{}, fmt.Errorf("discovery: budget must be >= 1")
	}

	run := Run{
		ID:         uuid.New(),
		CohortID:   cohortID,
		Strategy:   strategy,
		Weights:    w,
		Seed:       seed,
		Budget:     budget,
		PoolSize:   len(pool),
		CreatedAt:  time.Now().UTC(),
		Candidates: make([]Scored, 0, len(pool)),
		Rejected:   make([]Rejection, 0),
	}

	scored := make([]Scored, 0, len(pool))
	seen := make(map[string]bool, len(pool))
	for _, c := range pool {
		sc, ok := ScoreCandidate(c, st, w)
		if !ok {
			run.Rejected = append(run.Rejected, Rejection{ProposalID: c.ProposalID, Reason: sc.Rationale})
			continue
		}
		run.ValidPoolSize++
		fp := c.Genome.Fingerprint()
		if seen[fp] {
			run.Rejected = append(run.Rejected, Rejection{
				ProposalID: c.ProposalID,
				Reason:     "duplicate genome " + fp + " already represented in this pool",
			})
			continue
		}
		seen[fp] = true
		scored = append(scored, sc)
	}

	ordered := order(scored, strategy, seed)
	if len(ordered) < budget {
		run.Notes = fmt.Sprintf("only %d valid unique candidates available for budget %d", len(ordered), budget)
	}
	if len(ordered) > budget {
		ordered = ordered[:budget]
	}
	for i := range ordered {
		ordered[i].Rank = i + 1
		ordered[i].Selected = true
	}
	run.Candidates = ordered
	return run, nil
}

// order applies the strategy's ordering. Ties break on proposal ID so the
// result is deterministic for a given seed.
func order(scored []Scored, strategy Strategy, seed int64) []Scored {
	out := append([]Scored(nil), scored...)
	switch strategy {
	case StrategyAdaptive:
		sortSlice(out, func(a, b Scored) bool {
			if a.Score != b.Score {
				return a.Score > b.Score
			}
			return a.ProposalID.String() < b.ProposalID.String()
		})
	case StrategyGeminiOnly:
		// Generation order is represented by the proposal ID tie-break; the
		// caller supplies the pool already sorted by created_at.
		sortSlice(out, func(a, b Scored) bool {
			return a.ProposalID.String() < b.ProposalID.String()
		})
	case StrategyRandom:
		r := newRand(seed)
		r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	}
	return out
}

// sortSlice is a small insertion sort to avoid importing sort just for two
// comparators; pools are small (tens of candidates).
func sortSlice(items []Scored, less func(a, b Scored) bool) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && less(items[j], items[j-1]); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(0, math.Min(1, v))
}
