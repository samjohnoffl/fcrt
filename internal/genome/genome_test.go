package genome

import (
	"testing"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

func mkProposal(scenarioType, evasion string) scenario.Proposal {
	return scenario.Proposal{
		ID:                    uuid.New(),
		ScenarioType:          scenarioType,
		ExpectedEvasionVector: evasion,
	}
}

func TestEncodeIsDeterministic(t *testing.T) {
	p := scenario.Proposal{
		ID:                    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		ScenarioType:          "split_transaction",
		ExpectedEvasionVector: "structuring below threshold",
	}
	var a, b Genome
	Encode(&a, p)
	Encode(&b, p)
	if a != b {
		t.Fatalf("encoding not deterministic: %v != %v", a, b)
	}
}

func TestEncodeDimensionsInRange(t *testing.T) {
	p := mkProposal("layering", "shell company round-trip")
	var g Genome
	Encode(&g, p)
	for i, v := range g {
		if v < 0 || v > 1 {
			t.Errorf("dimension %d out of [0,1]: %f", i, v)
		}
	}
}

func TestDistanceIdenticalIsZero(t *testing.T) {
	var g Genome
	Encode(&g, mkProposal("split_transaction", "structuring"))
	if d := Distance(g, g); d != 0 {
		t.Errorf("distance to self = %f, want 0", d)
	}
}

func TestDistanceDifferentTypes(t *testing.T) {
	var a, b Genome
	Encode(&a, mkProposal("split_transaction", "structuring"))
	Encode(&b, mkProposal("rapid_movement", "rapid mule transfers"))
	if d := Distance(a, b); d <= 0 {
		t.Errorf("distance between different types = %f, want > 0", d)
	}
}

func TestRuleFocusWeights(t *testing.T) {
	cases := []struct {
		ev   string
		want float64
	}{
		{"structuring", 1.0},
		{"shell company", 1.0},
		{"rapid movement", 0.8},
		{"split payments", 0.7},
		{"unknown vector", 0.5},
		{"", 0.0},
	}
	for _, c := range cases {
		if got := ruleFocusWeight(c.ev); got != c.want {
			t.Errorf("ruleFocusWeight(%q) = %f, want %f", c.ev, got, c.want)
		}
	}
}

func TestDecode(t *testing.T) {
	var g Genome
	Encode(&g, mkProposal("layering", "shell company"))
	ruleFocus, st, bucket, novelty, gap := Decode(g)
	if ruleFocus == "" {
		t.Error("decoded ruleFocus empty")
	}
	if st != "layering" {
		t.Errorf("decoded scenarioType = %q, want layering", st)
	}
	if bucket < 0 || bucket > 7 {
		t.Errorf("bucket out of range: %d", bucket)
	}
	if novelty < 0 || novelty > 100 {
		t.Errorf("novelty out of range: %d", novelty)
	}
	if gap < 0 || gap > 1 {
		t.Errorf("gap out of range: %f", gap)
	}
	_ = scenario.Proposal{}
}
