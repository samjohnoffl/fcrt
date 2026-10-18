// Package benchmark compares candidate-selection strategies using stored
// discovery runs.
//
// This package deliberately contains no simulation and no constants that could
// flatter a strategy. Earlier versions returned fixed "approx 25% valid" style
// numbers; that made the research claim unfalsifiable and is not acceptable for
// an evidence-backed prototype. Every value here is derived from a persisted
// run or reported as unavailable.
package benchmark

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/discovery"
)

// StrategySummary is the measured outcome of one stored selection run.
type StrategySummary struct {
	Strategy     discovery.Strategy `json:"strategy"`
	RunID        uuid.UUID          `json:"run_id"`
	Weights      string             `json:"weights_version"`
	Seed         int64              `json:"seed"`
	Budget       int                `json:"budget"`
	PoolSize     int                `json:"pool_size"`
	ValidPool    int                `json:"valid_pool_size"`
	Selected     int                `json:"selected_candidates"`
	Approved     int                `json:"approved_candidates"`
	ValidYield   float64            `json:"valid_candidate_yield"`
	ApprovedRate float64            `json:"approved_rate"`
	CoverageGain float64            `json:"expected_coverage_gain"`
	Novelty      float64            `json:"mean_novelty"`
	Information  float64            `json:"mean_information_gain"`
	AverageScore float64            `json:"mean_adaptive_score"`
	RuntimeMS    int64              `json:"runtime_ms"`
}

// ComparisonResult is the side-by-side view returned by the API.
type ComparisonResult struct {
	CohortID         uuid.UUID          `json:"cohort_id"`
	Strategies       []StrategySummary  `json:"strategies"`
	Winner           discovery.Strategy `json:"winner,omitempty"`
	InsufficientData bool               `json:"insufficient_data"`
	Missing          []string           `json:"missing_strategies,omitempty"`
	Notes            string             `json:"notes"`
	ComputedAt       time.Time          `json:"computed_at"`
}

// strategyOrder fixes presentation order so the frontend is stable.
var strategyOrder = []discovery.Strategy{
	discovery.StrategyRandom,
	discovery.StrategyGeminiOnly,
	discovery.StrategyAdaptive,
}

// RunComparison builds a comparison from the latest stored run of each
// strategy for a cohort.
//
// It reports insufficient_data when any strategy has not been run. It does not
// substitute a default, and it does not declare a winner from partial data.
func RunComparison(ctx context.Context, repo *discovery.Repository, cohortID uuid.UUID) (ComparisonResult, error) {
	if repo == nil {
		return ComparisonResult{}, fmt.Errorf("benchmark: discovery repository is not configured")
	}

	runs, err := repo.LatestRuns(ctx, cohortID)
	if err != nil {
		return ComparisonResult{}, err
	}

	result := ComparisonResult{
		CohortID:   cohortID,
		Strategies: make([]StrategySummary, 0, len(strategyOrder)),
		ComputedAt: time.Now().UTC(),
	}

	for _, strategy := range strategyOrder {
		run, ok := runs[strategy]
		if !ok {
			result.Missing = append(result.Missing, string(strategy))
			continue
		}
		approved, err := repo.ApproveCount(ctx, run.ID)
		if err != nil {
			return ComparisonResult{}, fmt.Errorf("benchmark: approved count for %s: %w", strategy, err)
		}
		result.Strategies = append(result.Strategies, summarize(run, approved))
	}

	if len(result.Missing) > 0 {
		result.InsufficientData = true
		result.Notes = fmt.Sprintf(
			"No benchmark conclusion: missing stored runs for %s. "+
				"POST /api/v1/cohorts/{id}/discovery once per strategy with the same budget.",
			strings.Join(result.Missing, ", "))
		return result, nil
	}

	// A comparison over a handful of candidates cannot support a ranking
	// claim. Report it as descriptive rather than declaring a winner.
	const minSelectedForConclusion = 5
	belowPower := false
	for _, s := range result.Strategies {
		if s.Selected < minSelectedForConclusion {
			belowPower = true
			break
		}
	}

	winner, ranked := pickWinner(result.Strategies)
	switch {
	case belowPower:
		result.InsufficientData = true
		result.Notes = fmt.Sprintf(
			"Descriptive only: at least one strategy selected fewer than %d candidates. "+
				"Increase the discovery budget or the proposal pool before drawing a conclusion.",
			minSelectedForConclusion)
	case !ranked:
		result.InsufficientData = true
		result.Notes = "Statistical tie: all strategies produced identical metrics; no winner is declared."
	default:
		result.Winner = winner
		result.Notes = "Expected coverage gain is the mean coverage component of the selected " +
			"candidates, not a realised coverage delta; realised coverage requires executed " +
			"experiments. Information gain is a documented proxy (1 - unchanged-outcome share)."
	}
	return result, nil
}

// summarize derives metrics from a stored run.
func summarize(run discovery.Run, approved int) StrategySummary {
	s := StrategySummary{
		Strategy:  run.Strategy,
		RunID:     run.ID,
		Weights:   run.Weights.Version,
		Seed:      run.Seed,
		Budget:    run.Budget,
		PoolSize:  run.PoolSize,
		ValidPool: run.ValidPoolSize,
		Selected:  len(run.Candidates),
		Approved:  approved,
		RuntimeMS: run.DurationMS,
	}
	if run.PoolSize > 0 {
		s.ValidYield = float64(run.ValidPoolSize) / float64(run.PoolSize)
	}
	if s.Selected > 0 {
		s.ApprovedRate = float64(approved) / float64(s.Selected)
		s.CoverageGain = mean(run.Candidates, func(c discovery.Scored) float64 { return c.Components.Coverage })
		s.Novelty = mean(run.Candidates, func(c discovery.Scored) float64 { return c.Components.Novelty })
		s.Information = mean(run.Candidates, func(c discovery.Scored) float64 { return c.Components.Information })
		s.AverageScore = mean(run.Candidates, func(c discovery.Scored) float64 { return c.Score })
	}
	return s
}

func mean(candidates []discovery.Scored, value func(discovery.Scored) float64) float64 {
	if len(candidates) == 0 {
		return 0
	}
	total := 0.0
	for _, c := range candidates {
		total += value(c)
	}
	return total / float64(len(candidates))
}

// pickWinner ranks by expected coverage gain, then approval rate, then mean
// adaptive score. It returns ranked=false when every strategy is identical on
// all three, because a tie is not evidence for a winner.
func pickWinner(summaries []StrategySummary) (discovery.Strategy, bool) {
	best := summaries[0]
	allEqual := true
	for _, s := range summaries[1:] {
		if s.CoverageGain != summaries[0].CoverageGain ||
			s.ApprovedRate != summaries[0].ApprovedRate ||
			s.AverageScore != summaries[0].AverageScore {
			allEqual = false
		}
		if better(s, best) {
			best = s
		}
	}
	if allEqual {
		return "", false
	}
	return best.Strategy, true
}

func better(a, b StrategySummary) bool {
	if a.CoverageGain != b.CoverageGain {
		return a.CoverageGain > b.CoverageGain
	}
	if a.ApprovedRate != b.ApprovedRate {
		return a.ApprovedRate > b.ApprovedRate
	}
	return a.AverageScore > b.AverageScore
}
