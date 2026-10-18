package benchmark

import (
	"context"
	"fmt"
	"sync"
)

// ComparisonResult aggregates benchmark results across all three strategies.
// This is the output of the benchmark harness described in the project spec:
// it compares Random vs Gemini-only vs Adaptive candidate selection.
type ComparisonResult struct {
	NumCandidates int               `json:"num_candidates"`
	Strategies    []StrategySummary `json:"strategies"`
	Winner        BenchmarkStrategy `json:"winner"`
	Notes         string            `json:"notes,omitempty"`
}

// StrategySummary is a compact per-strategy summary for the comparison view.
type StrategySummary struct {
	Name                BenchmarkStrategy `json:"name"`
	ValidCandidateYield float64           `json:"valid_candidate_yield"`
	ApprovedRate        float64           `json:"approved_rate"`
	CoverageGain        float64           `json:"coverage_gain"`
	NoveltyScore        float64           `json:"novelty_score"`
	CostPerFinding      float64           `json:"cost_per_finding"`
	AverageScore        float64           `json:"average_adaptive_score,omitempty"`
	RuntimeMillis       int64             `json:"runtime_ms"`
}

// RunComparison runs all three strategies with the same configuration and
// produces a side-by-side comparison. This is the benchmark harness AC:
// "compare Random, Gemini-only, and Adaptive strategies on valid candidate
// yield, approved coverage, and cost per finding."
func RunComparison(ctx context.Context, cfg BenchmarkConfig) (ComparisonResult, error) {
	strategies := []BenchmarkStrategy{StrategyRandom, StrategyGeminiOnly, StrategyAdaptive}

	results := make([]BenchmarkResult, len(strategies))
	errs := make([]error, len(strategies))

	var wg sync.WaitGroup
	for i, s := range strategies {
		wg.Add(1)
		go func(idx int, strat BenchmarkStrategy) {
			defer wg.Done()
			c := cfg
			c.Strategy = strat
			// Deterministic seed per strategy for reproducibility.
			c.Seed = cfg.Seed + int64(idx)
			res, err := Run(ctx, c)
			results[idx] = res
			errs[idx] = err
		}(i, s)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return ComparisonResult{}, err
		}
	}

	comparison := ComparisonResult{
		NumCandidates: cfg.NumCandidates,
	}

	bestCoverage := -1.0
	for _, r := range results {
		summary := summarize(r)

		// cost per finding = runtime spend / approved findings
		if r.ApprovedCandidates > 0 {
			summary.CostPerFinding = float64(r.TotalRuntime.Milliseconds()) / float64(r.ApprovedCandidates)
		}

		comparison.Strategies = append(comparison.Strategies, summary)

		if r.CoverageGain > bestCoverage {
			bestCoverage = r.CoverageGain
			comparison.Winner = r.Strategy
		}
	}

	comparison.Notes = fmt.Sprintf(
		"Winner by coverage gain: %s (%.2f). Adaptive uses S(x)=wc·C+wn·N+wi·I+wv·V.",
		comparison.Winner, bestCoverage)

	return comparison, nil
}

// summarize converts a BenchmarkResult into a compact StrategySummary.
func summarize(r BenchmarkResult) StrategySummary {
	validYield := 0.0
	approvedRate := 0.0
	if r.TotalCandidates > 0 {
		validYield = float64(r.ValidCandidates) / float64(r.TotalCandidates)
		approvedRate = float64(r.ApprovedCandidates) / float64(r.TotalCandidates)
	}
	return StrategySummary{
		Name:                r.Strategy,
		ValidCandidateYield: validYield,
		ApprovedRate:        approvedRate,
		CoverageGain:        r.CoverageGain,
		NoveltyScore:        r.NoveltyScore,
		AverageScore:        r.AverageAdaptiveScore,
		RuntimeMillis:       r.TotalRuntime.Milliseconds(),
	}
}
