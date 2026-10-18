package benchmark

import (
	"testing"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/discovery"
	"github.com/yourorg/financial-crime-red-team/internal/genome"
	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

func run(strategy discovery.Strategy, coverage, novelty, info, score float64) discovery.Run {
	g := genome.Encode(scenario.Proposal{ScenarioType: "split_transaction", Description: "individual"})
	return discovery.Run{
		ID:            uuid.New(),
		Strategy:      strategy,
		Weights:       discovery.DefaultWeights(),
		Budget:        2,
		PoolSize:      4,
		ValidPoolSize: 3,
		Candidates: []discovery.Scored{
			{ProposalID: uuid.New(), Genome: g, GenomeValid: true, Score: score,
				Components: discovery.Components{Coverage: coverage, Novelty: novelty, Information: info, Validity: 1}},
			{ProposalID: uuid.New(), Genome: g, GenomeValid: true, Score: score,
				Components: discovery.Components{Coverage: coverage, Novelty: novelty, Information: info, Validity: 1}},
		},
	}
}

func TestSummarizeUsesStoredValues(t *testing.T) {
	s := summarize(run(discovery.StrategyAdaptive, 0.8, 0.6, 0.4, 0.7), 1)

	if s.Selected != 2 || s.PoolSize != 4 || s.ValidPool != 3 {
		t.Fatalf("counts not derived from run: %+v", s)
	}
	if s.ValidYield != 0.75 {
		t.Errorf("valid yield = %v, want 0.75", s.ValidYield)
	}
	if s.ApprovedRate != 0.5 {
		t.Errorf("approved rate = %v, want 0.5", s.ApprovedRate)
	}
	if s.CoverageGain != 0.8 || s.Novelty != 0.6 || s.Information != 0.4 || s.AverageScore != 0.7 {
		t.Errorf("component means wrong: %+v", s)
	}
}

func TestSummarizeHandlesEmptyRun(t *testing.T) {
	s := summarize(discovery.Run{Strategy: discovery.StrategyRandom}, 0)
	if s.ValidYield != 0 || s.ApprovedRate != 0 || s.CoverageGain != 0 {
		t.Fatalf("empty run produced non-zero metrics: %+v", s)
	}
}

func TestPickWinnerUsesCoverageThenApproval(t *testing.T) {
	low := StrategySummary{Strategy: discovery.StrategyRandom, CoverageGain: 0.2}
	high := StrategySummary{Strategy: discovery.StrategyAdaptive, CoverageGain: 0.9}
	if got, ok := pickWinner([]StrategySummary{low, high}); !ok || got != discovery.StrategyAdaptive {
		t.Fatalf("winner = %s (ok=%v), want adaptive", got, ok)
	}

	tieA := StrategySummary{Strategy: discovery.StrategyGeminiOnly, CoverageGain: 0.5, ApprovedRate: 0.1}
	tieB := StrategySummary{Strategy: discovery.StrategyAdaptive, CoverageGain: 0.5, ApprovedRate: 0.4}
	if got, ok := pickWinner([]StrategySummary{tieA, tieB}); !ok || got != discovery.StrategyAdaptive {
		t.Fatalf("tie-break winner = %s (ok=%v), want adaptive", got, ok)
	}
}

func TestPickWinnerDeclinesOnExactTie(t *testing.T) {
	a := StrategySummary{Strategy: discovery.StrategyRandom, CoverageGain: 0.5, ApprovedRate: 0.5, AverageScore: 0.5}
	b := StrategySummary{Strategy: discovery.StrategyAdaptive, CoverageGain: 0.5, ApprovedRate: 0.5, AverageScore: 0.5}
	if got, ok := pickWinner([]StrategySummary{a, b}); ok || got != "" {
		t.Fatalf("exact tie declared a winner: %q ok=%v", got, ok)
	}
}
