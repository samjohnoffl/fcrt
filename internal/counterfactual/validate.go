// Package counterfactual validates baseline/variant genome pairs.
//
// A counterfactual pair only means something when the difference between the
// two sides is the declared difference. This package performs the checks the
// design document requires:
//
//   - the changed dimension is explicit;
//   - unchanged dimensions are compared and deviations are reported;
//   - dependent changes (derived counterparties, graph edges) are listed;
//   - pairs with unintended changes are rejected;
//   - valid pairs are labelled tightly controlled, partially controlled or
//     exploratory.
//
// The package makes no causal claim. A valid pair supports "the declared
// change is associated with a different control outcome"; it does not prove
// that the change caused evasion.
package counterfactual

import (
	"fmt"
	"sort"

	"github.com/yourorg/financial-crime-red-team/internal/genome"
)

// Classification is the control level of a pair.
type Classification string

const (
	// TightlyControlled: one declared dimension changed and no dependent
	// dimension moved.
	TightlyControlled Classification = "tightly_controlled"
	// PartiallyControlled: one or two declared dimensions changed, or a
	// dependent dimension moved as a documented consequence.
	PartiallyControlled Classification = "partially_controlled"
	// Exploratory: three or more declared dimensions changed. The pair is
	// still useful, but it cannot isolate a single behaviour.
	Exploratory Classification = "exploratory"
	// Invalid: the comparison is rejected.
	Invalid Classification = "invalid"
)

// Dependencies maps a declared dimension to dimensions that may legitimately
// change as a derived consequence. For example, changing the transaction
// pattern changes the observed counterparty topology (network) even when the
// network dimension was not the target.
var Dependencies = map[string][]string{
	genome.DimTransactions:     {genome.DimNetwork},
	genome.DimGeography:        {genome.DimNetwork},
	genome.DimActors:           {genome.DimOwnership},
	genome.DimOwnership:        {genome.DimNetwork},
	genome.DimBehavioralChange: {genome.DimTransactions, genome.DimTemporal},
}

// Change records one dimension that differs between baseline and variant.
type Change struct {
	Dimension string `json:"dimension"`
	Baseline  string `json:"baseline"`
	Variant   string `json:"variant"`
	Declared  bool   `json:"declared"`
	DependsOn string `json:"depends_on,omitempty"`
}

// Report is the result of comparing a baseline and a variant genome.
type Report struct {
	Valid              bool           `json:"valid"`
	Classification     Classification `json:"classification"`
	DeclaredDimensions []string       `json:"declared_dimensions"`
	ChangedDimensions  []string       `json:"changed_dimensions"`
	Changes            []Change       `json:"changes"`
	DependentChanges   []Change       `json:"dependent_changes"`
	Violations         []string       `json:"violations"`
	Reasons            []string       `json:"reasons"`
}

// Compare validates a baseline/variant pair against the declared dimensions.
//
// declared lists the dimensions the experimenter claims to change. Passing an
// empty list is rejected: an implicit change is not a controlled comparison.
func Compare(baseline, variant genome.Genome, declared []string) Report {
	rep := Report{
		DeclaredDimensions: append([]string{}, declared...),
		Changes:            []Change{},
		DependentChanges:   []Change{},
		Violations:         []string{},
		Reasons:            []string{},
	}
	sort.Strings(rep.DeclaredDimensions)

	if baseline.SchemaVersion != variant.SchemaVersion {
		rep.Classification = Invalid
		rep.Violations = append(rep.Violations,
			fmt.Sprintf("genome schema mismatch: baseline %q, variant %q", baseline.SchemaVersion, variant.SchemaVersion))
		return rep
	}
	if len(declared) == 0 {
		rep.Classification = Invalid
		rep.Violations = append(rep.Violations, "no declared dimension: the changed dimension must be explicit")
		return rep
	}

	declaredSet := make(map[string]bool, len(declared))
	for _, d := range declared {
		if !knownDimension(d) {
			rep.Classification = Invalid
			rep.Violations = append(rep.Violations, fmt.Sprintf("unknown declared dimension %q", d))
			return rep
		}
		declaredSet[d] = true
	}

	changedDeclared := 0
	changedDependent := 0

	for _, name := range genome.DimensionNames {
		b := baseline.DimensionValue(name)
		v := variant.DimensionValue(name)
		if b == v {
			if declaredSet[name] {
				// A declared dimension that did not move means the pair does not
				// test what it says it tests.
				rep.Violations = append(rep.Violations,
					fmt.Sprintf("declared dimension %q is unchanged", name))
			}
			continue
		}

		ch := Change{Dimension: name, Baseline: b, Variant: v}
		rep.ChangedDimensions = append(rep.ChangedDimensions, name)

		if declaredSet[name] {
			ch.Declared = true
			changedDeclared++
			rep.Changes = append(rep.Changes, ch)
			continue
		}
		if dep, ok := dependencyOf(name, declaredSet); ok {
			ch.DependsOn = dep
			changedDependent++
			rep.Changes = append(rep.Changes, ch)
			rep.DependentChanges = append(rep.DependentChanges, ch)
			continue
		}

		// Undeclared and not a documented consequence: the pair is confounded.
		rep.Changes = append(rep.Changes, ch)
		rep.Violations = append(rep.Violations,
			fmt.Sprintf("unintended change in %q: %s -> %s", name, b, v))
	}

	if len(rep.Violations) > 0 {
		rep.Valid = false
		rep.Classification = Invalid
		return rep
	}

	rep.Valid = true
	switch {
	case changedDeclared == 1 && changedDependent == 0:
		rep.Classification = TightlyControlled
		rep.Reasons = append(rep.Reasons,
			"exactly one declared dimension changed and no derived dimension moved")
	case changedDeclared <= 2:
		rep.Classification = PartiallyControlled
		rep.Reasons = append(rep.Reasons, "one or two declared dimensions changed")
		if changedDependent > 0 {
			rep.Reasons = append(rep.Reasons,
				fmt.Sprintf("%d dependent dimension(s) moved as a documented consequence", changedDependent))
		}
	default:
		rep.Classification = Exploratory
		rep.Reasons = append(rep.Reasons,
			fmt.Sprintf("%d declared dimensions changed; the pair cannot isolate one behaviour", changedDeclared))
	}
	rep.Reasons = append(rep.Reasons,
		"validity is structural only: it does not establish that the declared change caused the outcome")
	return rep
}

// dependencyOf reports whether name is a documented consequence of any
// declared dimension. When several declared dimensions could explain it, the
// first in canonical order is reported.
func dependencyOf(name string, declared map[string]bool) (string, bool) {
	for _, d := range genome.DimensionNames {
		if !declared[d] {
			continue
		}
		for _, dep := range Dependencies[d] {
			if dep == name {
				return d, true
			}
		}
	}
	return "", false
}

func knownDimension(name string) bool {
	for _, d := range genome.DimensionNames {
		if d == name {
			return true
		}
	}
	return false
}
