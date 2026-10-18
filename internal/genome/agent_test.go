package genome

import (
	"testing"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

func proposals(n int) []scenario.Proposal {
	types := []string{"split_transaction", "rapid_movement", "layering", "other"}
	vectors := []string{"structuring", "rapid mule", "shell company", "generic evasion"}
	out := make([]scenario.Proposal, n)
	for i := 0; i < n; i++ {
		out[i] = scenario.Proposal{
			ID:                    uuid.New(),
			ScenarioType:          types[i%len(types)],
			ExpectedEvasionVector: vectors[i%len(vectors)],
		}
	}
	return out
}

func TestAgentEncodeBatch(t *testing.T) {
	a := NewAgent()
	ps := proposals(5)
	gs := a.EncodeBatch(ps)
	if len(gs) != 5 {
		t.Fatalf("got %d genomes, want 5", len(gs))
	}
}

func TestAgentScoreBatchNoveltyInRange(t *testing.T) {
	a := NewAgent()
	scored := a.ScoreBatch(proposals(6))
	if len(scored) != 6 {
		t.Fatalf("got %d scored, want 6", len(scored))
	}
	for _, s := range scored {
		if s.Novelty < 0 || s.Novelty > 1 {
			t.Errorf("novelty out of range: %f", s.Novelty)
		}
	}
}

func TestAgentSingleCandidateMaxNovelty(t *testing.T) {
	a := NewAgent()
	scored := a.ScoreBatch(proposals(1))
	if len(scored) != 1 || scored[0].Novelty != 1.0 {
		t.Errorf("single candidate should have novelty 1.0, got %v", scored)
	}
}

func TestAgentDistanceMatrixSymmetric(t *testing.T) {
	a := NewAgent()
	m := a.DistanceMatrix(proposals(4))
	if len(m) != 4 || len(m[0]) != 4 {
		t.Fatalf("matrix not 4x4")
	}
	for i := range m {
		if m[i][i] != 0 {
			t.Errorf("diagonal[%d] = %f, want 0", i, m[i][i])
		}
		for j := range m {
			if m[i][j] != m[j][i] {
				t.Errorf("matrix not symmetric at [%d][%d]", i, j)
			}
		}
	}
}

func TestAgentRankByNoveltyDescending(t *testing.T) {
	a := NewAgent()
	ranked := a.RankByNovelty(proposals(5))
	for i := 1; i < len(ranked); i++ {
		if ranked[i-1].Novelty < ranked[i].Novelty {
			t.Errorf("ranking not descending at %d: %f < %f", i, ranked[i-1].Novelty, ranked[i].Novelty)
		}
	}
}

func TestAgentSelectDiverseSize(t *testing.T) {
	a := NewAgent()
	sel := a.SelectDiverse(proposals(10), 3)
	if len(sel) != 3 {
		t.Fatalf("selected %d, want 3", len(sel))
	}
	// All distinct
	seen := map[string]bool{}
	for _, s := range sel {
		id := s.Proposal.ID.String()
		if seen[id] {
			t.Errorf("duplicate selection: %s", id)
		}
		seen[id] = true
	}
}

func TestAgentSelectDiverseMoreThanPool(t *testing.T) {
	a := NewAgent()
	sel := a.SelectDiverse(proposals(2), 10)
	if len(sel) != 2 {
		t.Errorf("selected %d, want 2 (capped at pool size)", len(sel))
	}
}

func TestAgentSelectDiverseEmpty(t *testing.T) {
	a := NewAgent()
	if sel := a.SelectDiverse(nil, 3); sel != nil {
		t.Errorf("expected nil for empty pool, got %v", sel)
	}
	if sel := a.SelectDiverse(proposals(3), 0); sel != nil {
		t.Errorf("expected nil for k=0, got %v", sel)
	}
}

func TestAgentCoverageSpread(t *testing.T) {
	a := NewAgent()
	if s := a.CoverageSpread(proposals(1)); s != 0 {
		t.Errorf("single candidate spread = %f, want 0", s)
	}
	s := a.CoverageSpread(proposals(4))
	if s <= 0 || s > 1 {
		t.Errorf("spread out of range: %f", s)
	}
}

func TestAgentFilterDuplicatesDisabled(t *testing.T) {
	a := NewAgent() // minNovelty = 0 => disabled
	in := proposals(4)
	out := a.FilterDuplicates(in)
	if len(out) != len(in) {
		t.Errorf("filter disabled should keep all, got %d of %d", len(out), len(in))
	}
}

func TestAgentFilterDuplicatesRemovesIdentical(t *testing.T) {
	a := NewAgent(WithMinNovelty(0.01))
	// Two candidates with identical scenario type AND evasion vector => novelty 0.
	p1 := scenario.Proposal{ID: uuid.New(), ScenarioType: "layering", ExpectedEvasionVector: "shell"}
	p2 := scenario.Proposal{ID: uuid.New(), ScenarioType: "layering", ExpectedEvasionVector: "shell"}
	p3 := scenario.Proposal{ID: uuid.New(), ScenarioType: "rapid_movement", ExpectedEvasionVector: "rapid"}
	out := a.FilterDuplicates([]scenario.Proposal{p1, p2, p3})
	if len(out) >= 3 {
		t.Errorf("expected duplicates removed, kept %d of 3", len(out))
	}
}
