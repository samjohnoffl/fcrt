package discovery

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/genome"
	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

func candidate(scenarioType, evasion, description string) Candidate {
	p := scenario.Proposal{
		ScenarioType:          scenarioType,
		ExpectedEvasionVector: evasion,
		Description:           description,
	}
	return Candidate{
		ProposalID:   uuid.New(),
		MutationType: scenarioType,
		ScenarioType: scenarioType,
		Genome:       genome.Encode(p),
	}
}

func TestDefaultWeightsValid(t *testing.T) {
	w := DefaultWeights()
	if err := w.Validate(); err != nil {
		t.Fatalf("default weights invalid: %v", err)
	}
	if got := w.Score(Components{1, 1, 1, 1}); got != 1.0 {
		t.Fatalf("all-ones score = %v, want 1", got)
	}
	bad := w
	bad.Coverage = 0.9
	if err := bad.Validate(); err == nil {
		t.Fatal("weights summing above 1 should be rejected")
	}
}

func TestCoverageScoreUnusedIsMax(t *testing.T) {
	st := NewState()
	if got := CoverageScore("split_transaction", st); got != 1.0 {
		t.Fatalf("unused mutation coverage = %v, want 1", got)
	}
	st.MutationCounts["split_transaction"] = 3
	st.TotalExperiments = 4
	if got := CoverageScore("split_transaction", st); got != 0.25 {
		t.Fatalf("well-tested mutation coverage = %v, want 0.25", got)
	}
}

func TestInformationScoreUsesOutcomeHistory(t *testing.T) {
	st := NewState()
	if got := InformationScore("split_transaction", st); got != 1.0 {
		t.Fatalf("no history information = %v, want 1", got)
	}
	st.Outcomes["split_transaction"] = OutcomeStat{Total: 4, Unchanged: 4, Changed: 0}
	if got := InformationScore("split_transaction", st); got != 0 {
		t.Fatalf("fully unchanged history information = %v, want 0", got)
	}
	st.Outcomes["split_transaction"] = OutcomeStat{Total: 4, Unchanged: 2, Changed: 2}
	if got := InformationScore("split_transaction", st); got != 0.5 {
		t.Fatalf("half-changed history information = %v, want 0.5", got)
	}
}

func TestScoreCandidateExplainsItself(t *testing.T) {
	c := candidate("split_transaction", "structuring below threshold", "individual splits deposits")
	sc, ok := ScoreCandidate(c, NewState(), DefaultWeights())
	if !ok {
		t.Fatal("valid candidate was rejected")
	}
	if !sc.GenomeValid {
		t.Error("genome should be marked valid")
	}
	for _, want := range []string{"S=", "C(", "N(", "I(", "V(", DefaultWeights().Version, c.Genome.GenomeID} {
		if !strings.Contains(sc.Rationale, want) {
			t.Errorf("rationale missing %q: %s", want, sc.Rationale)
		}
	}
	if sc.Components.Validity != 1.0 {
		t.Errorf("validity = %v, want 1", sc.Components.Validity)
	}
}

func TestSelectRejectsInvalidGenome(t *testing.T) {
	bad := candidate("split_transaction", "structuring", "individual")
	bad.Genome.SchemaVersion = "bogus"

	run, err := Select(uuid.New(), []Candidate{bad}, NewState(), DefaultWeights(), StrategyAdaptive, 5, 1)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(run.Candidates) != 0 {
		t.Fatalf("invalid candidate was selected: %+v", run.Candidates)
	}
	if len(run.Rejected) != 1 || !strings.Contains(run.Rejected[0].Reason, "validation failed") {
		t.Fatalf("invalid candidate rejection not recorded: %+v", run.Rejected)
	}
}

func TestSelectAdaptivePrefersUntestedMutation(t *testing.T) {
	tested := candidate("rapid_movement", "rapid pass-through", "company")
	untested := candidate("layering", "multi-hop layering chain", "shell company")

	st := NewState()
	st.MutationCounts["rapid_movement"] = 5
	st.TotalExperiments = 5

	run, err := Select(uuid.New(), []Candidate{tested, untested}, st, DefaultWeights(), StrategyAdaptive, 1, 1)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(run.Candidates) != 1 {
		t.Fatalf("selected %d candidates, want 1", len(run.Candidates))
	}
	if run.Candidates[0].ProposalID != untested.ProposalID {
		t.Fatal("adaptive selection did not prefer the untested mutation type")
	}
	if run.Candidates[0].Rank != 1 || !run.Candidates[0].Selected {
		t.Error("selected candidate missing rank/selected flags")
	}
}

func TestSelectCollapsesDuplicateGenomes(t *testing.T) {
	a := candidate("split_transaction", "structuring below threshold", "individual splits deposits")
	b := a
	b.ProposalID = uuid.New()

	run, err := Select(uuid.New(), []Candidate{a, b}, NewState(), DefaultWeights(), StrategyAdaptive, 5, 1)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(run.Candidates) != 1 {
		t.Fatalf("duplicate genomes produced %d candidates, want 1", len(run.Candidates))
	}
	if len(run.Rejected) != 1 || !strings.Contains(run.Rejected[0].Reason, "duplicate genome") {
		t.Fatalf("duplicate rejection not recorded: %+v", run.Rejected)
	}
}

func TestSelectRandomIsReproducible(t *testing.T) {
	pool := []Candidate{
		candidate("split_transaction", "a", "individual"),
		candidate("rapid_movement", "b", "company"),
		candidate("layering", "c", "shell"),
	}
	first, err := Select(uuid.Nil, pool, NewState(), DefaultWeights(), StrategyRandom, 2, 42)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	second, err := Select(uuid.Nil, pool, NewState(), DefaultWeights(), StrategyRandom, 2, 42)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	for i := range first.Candidates {
		if first.Candidates[i].ProposalID != second.Candidates[i].ProposalID {
			t.Fatal("same seed produced a different random selection")
		}
	}
}

func TestSelectEnforcesBudgetAndRecordsShortfall(t *testing.T) {
	pool := []Candidate{
		candidate("split_transaction", "a", "individual"),
		candidate("layering", "c", "shell"),
	}
	run, err := Select(uuid.Nil, pool, NewState(), DefaultWeights(), StrategyGeminiOnly, 5, 1)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(run.Candidates) != 2 {
		t.Fatalf("selected %d, want 2", len(run.Candidates))
	}
	if !strings.Contains(run.Notes, "only 2 valid unique candidates") {
		t.Fatalf("shortfall not recorded: %q", run.Notes)
	}
}

func TestSelectRejectsUnknownStrategy(t *testing.T) {
	if _, err := Select(uuid.Nil, nil, NewState(), DefaultWeights(), Strategy("magic"), 1, 1); err == nil {
		t.Fatal("unknown strategy accepted")
	}
}
