// Package regression implements the Regression Test Pack: a portable, versioned
// collection of reproducible experiment cases derived from evidence bundles.
//
// It satisfies PRODUCT_SPEC item 7:
//
//	"Regression: re-run any evidence bundle deterministically and assert
//	 identical AML diff."
//
// A Pack is self-describing (version + AML rule version + checksum) and can be
// exported to / imported from JSON so regression suites can be checked into
// version control and executed in CI without a live database.
package regression

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/google/uuid"
)

// PackVersion is the schema version of the regression pack format.
const PackVersion = "1.0"

// ExpectedDiff is the assertion attached to a regression case: what the AML
// evaluator produced the first time the experiment ran.
type ExpectedDiff struct {
	EvasionDelta        int      `json:"evasion_delta"`
	NewAlertRuleIDs     []string `json:"new_alert_rule_ids"`
	DroppedAlertRuleIDs []string `json:"dropped_alert_rule_ids"`
	OutcomeUnchanged    bool     `json:"outcome_unchanged"`
}

// Case is a single reproducible regression test.
//
// It holds the minimal inputs required to deterministically re-execute the
// experiment (cohort reference, frozen manifest hash, mutation parameters) plus
// the expected AML diff to assert against.
type Case struct {
	ExperimentID   uuid.UUID `json:"experiment_id"`
	Name           string    `json:"name"`
	CohortID       uuid.UUID `json:"cohort_id"`
	ManifestHash   string    `json:"manifest_hash"`
	ControlHash    string    `json:"control_hash"`
	WindowStart    time.Time `json:"window_start"`
	WindowEnd      time.Time `json:"window_end"`
	DeclaredChange string    `json:"declared_change"`
	MutationType   string    `json:"mutation_type"`
	// MutationSpec is an open-schema mutation parameter blob.
	// any is required here: the mutation schema differs per mutation type.
	MutationSpec map[string]any `json:"mutation_spec"`
	Expected     ExpectedDiff   `json:"expected"`
	// SourceChecksum is the evidence-bundle checksum this case was derived from.
	SourceChecksum string `json:"source_checksum"`
}

// Pack is a portable, versioned collection of regression cases.
type Pack struct {
	Version     string    `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
	AMLVersion  string    `json:"aml_rule_version"`
	Cases       []Case    `json:"cases"`
	// Checksum is SHA-256 of the canonical JSON of the Pack with Checksum == "".
	Checksum string `json:"checksum"`
}

// Build assembles a Pack from cases, stamping the version and a content checksum.
// Cases are sorted by (Name, ExperimentID) for a stable, diff-friendly file.
func Build(cases []Case, amlVersion string, generatedAt time.Time) (Pack, error) {
	sorted := make([]Case, len(cases))
	copy(sorted, cases)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].ExperimentID.String() < sorted[j].ExperimentID.String()
	})

	p := Pack{
		Version:     PackVersion,
		GeneratedAt: generatedAt.UTC(),
		AMLVersion:  amlVersion,
		Cases:       sorted,
	}
	sum, err := p.computeChecksum()
	if err != nil {
		return Pack{}, err
	}
	p.Checksum = sum
	return p, nil
}

// computeChecksum hashes the canonical (Checksum-cleared) JSON encoding.
func (p Pack) computeChecksum() (string, error) {
	p.Checksum = ""
	b, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("regression: marshal pack: %w", err)
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum), nil
}

// VerifyChecksum recomputes the checksum and reports whether it matches.
func (p Pack) VerifyChecksum() (bool, error) {
	got, err := p.computeChecksum()
	if err != nil {
		return false, err
	}
	return got == p.Checksum, nil
}

// Export writes the pack as indented JSON to w.
func (p Pack) Export(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}

// Import reads a pack from JSON and validates its version and checksum.
func Import(r io.Reader) (Pack, error) {
	var p Pack
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Pack{}, fmt.Errorf("regression: decode pack: %w", err)
	}
	if p.Version == "" {
		return Pack{}, fmt.Errorf("regression: pack version missing")
	}
	if p.Version != PackVersion {
		return Pack{}, fmt.Errorf("regression: unsupported pack version %q (want %q)", p.Version, PackVersion)
	}
	ok, err := p.VerifyChecksum()
	if err != nil {
		return Pack{}, err
	}
	if !ok {
		return Pack{}, fmt.Errorf("regression: pack checksum mismatch (file may be corrupted or tampered)")
	}
	return p, nil
}

// ─── Comparison ───────────────────────────────────────────────────────────────

// CaseResult is the outcome of evaluating one regression case.
type CaseResult struct {
	ExperimentID uuid.UUID    `json:"experiment_id"`
	Name         string       `json:"name"`
	Passed       bool         `json:"passed"`
	Mismatches   []string     `json:"mismatches,omitempty"`
	Expected     ExpectedDiff `json:"expected"`
	Actual       ExpectedDiff `json:"actual"`
}

// CompareCase asserts an actual AML diff against a case's expectation.
// The comparison is order-insensitive on rule-ID lists.
func CompareCase(c Case, actual ExpectedDiff) CaseResult {
	res := CaseResult{
		ExperimentID: c.ExperimentID,
		Name:         c.Name,
		Expected:     c.Expected,
		Actual:       actual,
		Passed:       true,
	}

	if c.Expected.EvasionDelta != actual.EvasionDelta {
		res.Mismatches = append(res.Mismatches,
			fmt.Sprintf("evasion_delta: expected %d, got %d", c.Expected.EvasionDelta, actual.EvasionDelta))
	}
	if c.Expected.OutcomeUnchanged != actual.OutcomeUnchanged {
		res.Mismatches = append(res.Mismatches,
			fmt.Sprintf("outcome_unchanged: expected %t, got %t", c.Expected.OutcomeUnchanged, actual.OutcomeUnchanged))
	}
	if !sameSet(c.Expected.NewAlertRuleIDs, actual.NewAlertRuleIDs) {
		res.Mismatches = append(res.Mismatches,
			fmt.Sprintf("new_alert_rule_ids: expected %v, got %v", c.Expected.NewAlertRuleIDs, actual.NewAlertRuleIDs))
	}
	if !sameSet(c.Expected.DroppedAlertRuleIDs, actual.DroppedAlertRuleIDs) {
		res.Mismatches = append(res.Mismatches,
			fmt.Sprintf("dropped_alert_rule_ids: expected %v, got %v", c.Expected.DroppedAlertRuleIDs, actual.DroppedAlertRuleIDs))
	}

	res.Passed = len(res.Mismatches) == 0
	return res
}

// sameSet reports whether two string slices contain the same elements
// (order-insensitive, duplicates ignored).
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa := append([]string(nil), a...)
	sb := append([]string(nil), b...)
	sort.Strings(sa)
	sort.Strings(sb)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}
