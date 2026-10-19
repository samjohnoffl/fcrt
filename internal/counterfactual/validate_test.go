package counterfactual

import (
	"strings"
	"testing"

	"github.com/yourorg/financial-crime-red-team/internal/genome"
)

func baseGenome() genome.Genome {
	g := genome.Genome{
		SchemaVersion: genome.SchemaVersion,
		SyntheticOnly: true,
		Dimensions: genome.Dimensions{
			Actors:            "company",
			Accounts:          "active",
			Transactions:      "structuring",
			TransactionCount:  6,
			Temporal:          "burst",
			Network:           "star",
			CounterpartyCount: 3,
			Ownership:         "single_controller",
			Geography:         "domestic",
			BehavioralChange:  "increase_velocity",
			ControlContext:    "velocity",
		},
	}
	g.GenomeID = g.Fingerprint()
	return g
}

func TestCompare_TightlyControlled(t *testing.T) {
	baseline := baseGenome()
	variant := baseGenome()
	variant.Dimensions.TransactionCount = 12
	variant.GenomeID = variant.Fingerprint()

	rep := Compare(baseline, variant, []string{genome.DimTransactions})
	if !rep.Valid {
		t.Fatalf("valid=false, violations=%v", rep.Violations)
	}
	if rep.Classification != TightlyControlled {
		t.Errorf("classification=%q, want tightly_controlled", rep.Classification)
	}
	if len(rep.ChangedDimensions) != 1 || rep.ChangedDimensions[0] != genome.DimTransactions {
		t.Errorf("changed=%v, want [transactions]", rep.ChangedDimensions)
	}
}

func TestCompare_DependentChangeIsReportedNotRejected(t *testing.T) {
	baseline := baseGenome()
	variant := baseGenome()
	variant.Dimensions.TransactionCount = 12
	variant.Dimensions.CounterpartyCount = 9 // derived from the new pattern
	variant.GenomeID = variant.Fingerprint()

	rep := Compare(baseline, variant, []string{genome.DimTransactions})
	if !rep.Valid {
		t.Fatalf("valid=false, violations=%v", rep.Violations)
	}
	if rep.Classification != PartiallyControlled {
		t.Errorf("classification=%q, want partially_controlled", rep.Classification)
	}
	if len(rep.DependentChanges) != 1 {
		t.Fatalf("dependent changes=%d, want 1", len(rep.DependentChanges))
	}
	d := rep.DependentChanges[0]
	if d.Dimension != genome.DimNetwork || d.DependsOn != genome.DimTransactions {
		t.Errorf("dependent change=%+v, want network derived from transactions", d)
	}
}

func TestCompare_UnintendedChangeIsRejected(t *testing.T) {
	baseline := baseGenome()
	variant := baseGenome()
	variant.Dimensions.TransactionCount = 12
	variant.Dimensions.Geography = "high_risk_jurisdiction" // not declared, not dependent
	variant.GenomeID = variant.Fingerprint()

	rep := Compare(baseline, variant, []string{genome.DimTransactions})
	if rep.Valid {
		t.Fatal("valid=true, want rejection for an undeclared geography change")
	}
	if rep.Classification != Invalid {
		t.Errorf("classification=%q, want invalid", rep.Classification)
	}
	if len(rep.Violations) != 1 || !strings.Contains(rep.Violations[0], "geography") {
		t.Errorf("violations=%v, want one geography violation", rep.Violations)
	}
}

func TestCompare_DeclaredDimensionUnchangedIsRejected(t *testing.T) {
	baseline := baseGenome()
	variant := baseGenome()
	variant.Dimensions.Accounts = "dormant"
	variant.GenomeID = variant.Fingerprint()

	rep := Compare(baseline, variant, []string{genome.DimGeography})
	if rep.Valid {
		t.Fatal("valid=true, want rejection: geography was declared but did not change")
	}
	joined := strings.Join(rep.Violations, "; ")
	if !strings.Contains(joined, "declared dimension") || !strings.Contains(joined, "unchanged") {
		t.Errorf("violations=%v, want declared-but-unchanged", rep.Violations)
	}
	if !strings.Contains(joined, "unintended change") {
		t.Errorf("violations=%v, want the undeclared accounts change reported too", rep.Violations)
	}
}

func TestCompare_ThreeDeclaredDimensionsIsExploratory(t *testing.T) {
	baseline := baseGenome()
	variant := baseGenome()
	variant.Dimensions.TransactionCount = 12
	variant.Dimensions.Geography = "cross_border"
	variant.Dimensions.Temporal = "sustained"
	variant.GenomeID = variant.Fingerprint()

	rep := Compare(baseline, variant, []string{
		genome.DimTransactions, genome.DimGeography, genome.DimTemporal,
	})
	if !rep.Valid {
		t.Fatalf("valid=false, violations=%v", rep.Violations)
	}
	if rep.Classification != Exploratory {
		t.Errorf("classification=%q, want exploratory", rep.Classification)
	}
}

func TestCompare_NoDeclaredDimensionIsRejected(t *testing.T) {
	g := baseGenome()
	rep := Compare(g, g, nil)
	if rep.Valid || rep.Classification != Invalid {
		t.Fatalf("valid=%v classification=%q, want invalid", rep.Valid, rep.Classification)
	}
}

func TestCompare_SchemaMismatchIsRejected(t *testing.T) {
	baseline := baseGenome()
	variant := baseGenome()
	variant.SchemaVersion = "1.0.0"
	variant.Dimensions.TransactionCount = 12

	rep := Compare(baseline, variant, []string{genome.DimTransactions})
	if rep.Valid || rep.Classification != Invalid {
		t.Fatalf("valid=%v classification=%q, want invalid", rep.Valid, rep.Classification)
	}
}
