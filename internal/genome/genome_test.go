package genome

import (
	"strings"
	"testing"

	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

func proposal(scenarioType, evasion, description string) scenario.Proposal {
	return scenario.Proposal{
		ScenarioType:          scenarioType,
		ExpectedEvasionVector: evasion,
		Description:           description,
		SuggestedTxPattern: []scenario.TxPatternStep{
			{"step": float64(1), "counterparty": "acct-a"},
			{"step": float64(2), "counterparty": "acct-b"},
		},
	}
}

func TestEncodeIsDeterministicAndValid(t *testing.T) {
	p := proposal("split_transaction", "structuring below threshold", "individual splits deposits")

	a := Encode(p)
	b := Encode(p)

	if a.GenomeID != b.GenomeID {
		t.Fatalf("same proposal encoded to different genome IDs: %s vs %s", a.GenomeID, b.GenomeID)
	}
	if err := a.Validate(); err != nil {
		t.Fatalf("encoded genome failed validation: %v", err)
	}
	if !a.SyntheticOnly {
		t.Fatal("encoded genome must be marked synthetic_only")
	}
	if a.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %q, want %q", a.SchemaVersion, SchemaVersion)
	}
}

func TestEncodeDerivesNineDimensions(t *testing.T) {
	g := Encode(proposal("rapid_movement", "rapid pass-through of funds", "company moves funds quickly"))

	if got := g.Dimensions.Transactions; got != "rapid_passthrough" {
		t.Errorf("transactions = %q, want rapid_passthrough", got)
	}
	if got := g.Dimensions.ControlContext; got != "rapid_movement_v1" {
		t.Errorf("control_context = %q, want rapid_movement_v1", got)
	}
	if got := g.Dimensions.Actors; got != "company" {
		t.Errorf("actors = %q, want company", got)
	}
	if got := g.Dimensions.TransactionCount; got != 2 {
		t.Errorf("transaction_count = %d, want 2", got)
	}
	if got := g.Dimensions.CounterpartyCount; got != 2 {
		t.Errorf("counterparty_count = %d, want 2", got)
	}
	if g.ReferenceTypologyID != "typology.rapid_movement.v1" {
		t.Errorf("reference typology = %q", g.ReferenceTypologyID)
	}
}

func TestEncodeUsesUnknownRatherThanGuessing(t *testing.T) {
	g := Encode(scenario.Proposal{ScenarioType: "other", ExpectedEvasionVector: "something vague"})
	if g.Dimensions.Actors != unknown {
		t.Errorf("actors = %q, want %q", g.Dimensions.Actors, unknown)
	}
	if g.Dimensions.Geography != unknown {
		t.Errorf("geography = %q, want %q", g.Dimensions.Geography, unknown)
	}
}

func TestDistanceIdenticalIsZero(t *testing.T) {
	g := Encode(proposal("layering", "multi-hop layering chain", "shell company"))
	if d := Distance(g, g); d != 0 {
		t.Fatalf("distance to self = %v, want 0", d)
	}
}

func TestDistanceComponentsExposeInputs(t *testing.T) {
	a := Encode(proposal("split_transaction", "structuring below threshold", "individual"))
	b := Encode(proposal("rapid_movement", "rapid pass-through", "company"))

	comps := DistanceComponents(a, b)
	for _, name := range DimensionNames {
		if _, ok := comps[name]; !ok {
			t.Errorf("missing distance component %q", name)
		}
	}
	total, ok := comps["total"]
	if !ok {
		t.Fatal("missing total component")
	}
	if total <= 0 || total > 1 {
		t.Fatalf("total distance = %v, want (0,1]", total)
	}
	if comps[DimTransactions] <= 0 {
		t.Error("transactions dimension should differ between split and rapid movement")
	}
}

func TestNovelty(t *testing.T) {
	g := Encode(proposal("split_transaction", "structuring", "individual"))
	h := Encode(proposal("layering", "layering", "shell"))

	if got := Novelty(g, nil); got != 1.0 {
		t.Errorf("novelty with no priors = %v, want 1", got)
	}
	if got := Novelty(g, []Genome{g}); got != 0 {
		t.Errorf("novelty against identical prior = %v, want 0", got)
	}
	if got := Novelty(g, []Genome{h}); got <= 0 || got > 1 {
		t.Errorf("novelty against different prior = %v, want (0,1]", got)
	}
}

func TestValidateRejectsBadGenomes(t *testing.T) {
	valid := Encode(proposal("split_transaction", "structuring", "individual"))

	cases := map[string]func(g *Genome){
		"missing schema version": func(g *Genome) { g.SchemaVersion = "" },
		"wrong schema version":   func(g *Genome) { g.SchemaVersion = "1.0" },
		"not synthetic":          func(g *Genome) { g.SyntheticOnly = false },
		"empty dimension":        func(g *Genome) { g.Dimensions.Geography = "" },
		"zero tx count":          func(g *Genome) { g.Dimensions.TransactionCount = 0 },
		"zero counterparties":    func(g *Genome) { g.Dimensions.CounterpartyCount = 0 },
	}
	for name, mutate := range cases {
		g := valid
		mutate(&g)
		if err := g.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}

func TestFingerprintChangesWithDimensions(t *testing.T) {
	a := Encode(proposal("split_transaction", "structuring", "individual"))
	b := a
	b.Dimensions.Geography = "high_risk"

	if a.Fingerprint() == b.Fingerprint() {
		t.Fatal("fingerprint did not change when a dimension changed")
	}
	if !strings.HasPrefix(a.Fingerprint(), "g_") {
		t.Fatalf("fingerprint = %q, want g_ prefix", a.Fingerprint())
	}
}
