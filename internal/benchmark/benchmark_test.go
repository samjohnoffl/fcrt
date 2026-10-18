package benchmark

import (
	"context"
	"testing"
)

func TestRunRandom(t *testing.T) {
	res, err := Run(context.Background(), BenchmarkConfig{
		Strategy:      StrategyRandom,
		NumCandidates: 100,
		Seed:          42,
	})
	if err != nil {
		t.Fatalf("Run(random) error: %v", err)
	}
	if res.Strategy != StrategyRandom {
		t.Errorf("strategy = %s, want random", res.Strategy)
	}
	if res.TotalCandidates != 100 {
		t.Errorf("total = %d, want 100", res.TotalCandidates)
	}
	if res.ValidCandidates+res.ApprovedCandidates+res.RejectedCandidates != 100 {
		t.Errorf("candidate buckets do not sum to total")
	}
}

func TestRunUnknownStrategy(t *testing.T) {
	_, err := Run(context.Background(), BenchmarkConfig{
		Strategy:      "nonsense",
		NumCandidates: 10,
	})
	if err == nil {
		t.Fatal("expected error for unknown strategy")
	}
}

func TestRunComparison(t *testing.T) {
	cmp, err := RunComparison(context.Background(), BenchmarkConfig{
		NumCandidates: 120,
		Seed:          7,
	})
	if err != nil {
		t.Fatalf("RunComparison error: %v", err)
	}
	if len(cmp.Strategies) != 3 {
		t.Fatalf("strategies = %d, want 3", len(cmp.Strategies))
	}
	if cmp.Winner == "" {
		t.Error("winner not set")
	}
	// Adaptive should win by coverage gain in our model.
	if cmp.Winner != StrategyAdaptive {
		t.Errorf("winner = %s, want adaptive", cmp.Winner)
	}
	for _, s := range cmp.Strategies {
		if s.ValidCandidateYield < 0 || s.ValidCandidateYield > 1 {
			t.Errorf("%s valid yield out of range: %f", s.Name, s.ValidCandidateYield)
		}
		if s.CoverageGain < 0 {
			t.Errorf("%s coverage gain negative", s.Name)
		}
	}
}

func TestComparisonDeterministic(t *testing.T) {
	cfg := BenchmarkConfig{NumCandidates: 60, Seed: 99}
	a, err := RunComparison(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RunComparison(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if a.Winner != b.Winner || len(a.Strategies) != len(b.Strategies) {
		t.Error("comparison not deterministic")
	}
}
