// Package genome implements the Financial Crime Genome: a structured,
// machine-readable description of a candidate financial-crime behaviour.
//
// The genome is the differentiator described in the project report. It is
// deliberately NOT a free-text prompt and NOT an opaque embedding. It exposes
// nine named dimensions that can be compared, mutated one at a time, and shown
// to a reviewer. Every dimension is either a stable categorical code or a
// bounded integer magnitude, so two genomes are always comparable.
//
// Two disclaimers are part of the contract, not decoration:
//
//   - A genome describes a *candidate*, not a finding. Validation and control
//     outcomes decide whether a candidate is useful.
//   - Genome similarity is not proof of novelty or of coverage. It is one input
//     to the adaptive score, which is why the score exposes its components.
package genome

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

// SchemaVersion is the genome schema version. It is bumped whenever the
// meaning of a dimension changes, so stored genomes remain interpretable.
const SchemaVersion = "2.0.0"

// The nine dimensions required by the design document.
const (
	DimActors           = "actors"
	DimAccounts         = "accounts"
	DimTransactions     = "transactions"
	DimTemporal         = "temporal"
	DimNetwork          = "network"
	DimOwnership        = "ownership"
	DimGeography        = "geography"
	DimBehavioralChange = "behavioral_change"
	DimControlContext   = "control_context"
)

// DimensionNames lists the nine dimensions in canonical order. Used for
// stable output ordering and for the distance component map.
var DimensionNames = []string{
	DimActors,
	DimAccounts,
	DimTransactions,
	DimTemporal,
	DimNetwork,
	DimOwnership,
	DimGeography,
	DimBehavioralChange,
	DimControlContext,
}

// Genome is the structured representation of one candidate behaviour.
//
// GenomeID is derived deterministically from the dimensions (see Fingerprint),
// so re-encoding the same proposal yields the same ID.
type Genome struct {
	GenomeID            string     `json:"genome_id"`
	SchemaVersion       string     `json:"schema_version"`
	ReferenceTypologyID string     `json:"reference_typology_id,omitempty"`
	Dimensions          Dimensions `json:"dimensions"`
	Constraints         []string   `json:"constraints,omitempty"`
	SyntheticOnly       bool       `json:"synthetic_only"`
}

// Dimensions holds the nine dimensions. Two dimensions (Transactions and
// Network) carry an integer magnitude alongside their categorical code; the
// other seven are pure categorical. The fields are grouped so the JSON shape
// mirrors the document's dimension table.
type Dimensions struct {
	// Actors — who performs the behaviour (individual, company, shell, mixed).
	Actors string `json:"actors"`
	// Accounts — the account lifecycle state the behaviour relies on.
	Accounts string `json:"accounts"`
	// Transactions — the transaction pattern, plus its step count.
	Transactions     string `json:"transactions"`
	TransactionCount int    `json:"transaction_count"`
	// Temporal — how the activity is spaced over time.
	Temporal string `json:"temporal"`
	// Network — the counterparty topology, plus the counterparty count.
	Network           string `json:"network"`
	CounterpartyCount int    `json:"counterparty_count"`
	// Ownership — whether accounts share a synthetic controller.
	Ownership string `json:"ownership"`
	// Geography — domestic, cross-border, or high-risk jurisdiction.
	Geography string `json:"geography"`
	// BehavioralChange — the before/after state transition being tested.
	BehavioralChange string `json:"behavioral_change"`
	// ControlContext — the control family the candidate is aimed at.
	ControlContext string `json:"control_context"`
}

// unknown is the explicit value used when a proposal does not state a
// dimension. The design document requires missing data to be represented as
// unknown rather than inferred silently.
const unknown = "unknown"

// Encode derives a genome from a Gemini proposal.
//
// Encoding is deterministic and rule-based: the same proposal always produces
// the same genome. It is intentionally conservative — ambiguous text maps to
// "unknown" rather than guessing. The proposal is never sent back to a model
// for encoding, so the genome does not inherit a second layer of hallucination.
func Encode(p scenario.Proposal) Genome {
	text := strings.ToLower(strings.Join([]string{
		p.ScenarioType,
		p.ExpectedEvasionVector,
		p.Description,
	}, " "))

	d := Dimensions{
		Actors:            classifyActors(text),
		Accounts:          classifyAccounts(text),
		Transactions:      classifyTransactions(p.ScenarioType, text),
		TransactionCount:  len(p.SuggestedTxPattern),
		Temporal:          classifyTemporal(text),
		Network:           classifyNetwork(text),
		CounterpartyCount: countCounterparties(p.SuggestedTxPattern),
		Ownership:         classifyOwnership(text),
		Geography:         classifyGeography(text),
		BehavioralChange:  classifyBehavioralChange(text),
		ControlContext:    classifyControlContext(text),
	}
	if d.TransactionCount == 0 {
		d.TransactionCount = 1
	}
	if d.CounterpartyCount == 0 {
		d.CounterpartyCount = 1
	}

	g := Genome{
		SchemaVersion:       SchemaVersion,
		ReferenceTypologyID: referenceTypology(p.ScenarioType),
		Dimensions:          d,
		Constraints:         []string{"synthetic_only"},
		SyntheticOnly:       true,
	}
	g.GenomeID = g.Fingerprint()
	return g
}

// Fingerprint returns a stable identifier for the genome's dimensions.
// It deliberately excludes GenomeID itself to avoid recursion.
func (g Genome) Fingerprint() string {
	var b strings.Builder
	b.WriteString(g.SchemaVersion)
	for _, name := range DimensionNames {
		b.WriteByte('|')
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(g.DimensionValue(name))
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "g_" + hex.EncodeToString(sum[:12])
}

// DimensionValue returns the comparable string form of a dimension. Numeric
// magnitudes are included so a change in step/counterparty count is visible.
func (g Genome) DimensionValue(name string) string {
	d := g.Dimensions
	switch name {
	case DimActors:
		return d.Actors
	case DimAccounts:
		return d.Accounts
	case DimTransactions:
		return fmt.Sprintf("%s/%d", d.Transactions, d.TransactionCount)
	case DimTemporal:
		return d.Temporal
	case DimNetwork:
		return fmt.Sprintf("%s/%d", d.Network, d.CounterpartyCount)
	case DimOwnership:
		return d.Ownership
	case DimGeography:
		return d.Geography
	case DimBehavioralChange:
		return d.BehavioralChange
	case DimControlContext:
		return d.ControlContext
	default:
		return ""
	}
}

// Validate checks the genome against the schema. A genome that fails
// validation must not reach the scorer: the design document requires invalid
// candidates to be rejected with a reason, never silently scored.
func (g Genome) Validate() error {
	if g.SchemaVersion == "" {
		return errors.New("genome: schema_version is required")
	}
	if g.SchemaVersion != SchemaVersion {
		return fmt.Errorf("genome: unsupported schema_version %q (want %q)", g.SchemaVersion, SchemaVersion)
	}
	if !g.SyntheticOnly {
		return errors.New("genome: synthetic_only must be true")
	}
	for _, name := range DimensionNames {
		if strings.TrimSpace(g.DimensionValue(name)) == "" {
			return fmt.Errorf("genome: dimension %q is empty", name)
		}
	}
	if g.Dimensions.TransactionCount < 1 {
		return errors.New("genome: transaction_count must be >= 1")
	}
	if g.Dimensions.CounterpartyCount < 1 {
		return errors.New("genome: counterparty_count must be >= 1")
	}
	return nil
}

// ─── categorical classifiers ─────────────────────────────────────────────────

func classifyActors(text string) string {
	switch {
	case has(text, "shell", "front company", "nominee", "straw"):
		return "shell"
	case has(text, "company", "corporate", "business", "entity", "merchant", "firm"):
		return "company"
	case has(text, "individual", "person", "customer", "retail", "mule"):
		return "individual"
	default:
		return unknown
	}
}

func classifyAccounts(text string) string {
	switch {
	case has(text, "dormant", "inactive", "reactivat"):
		return "dormant_to_active"
	case has(text, "new account", "newly opened", "opened recently", "fresh account"):
		return "newly_opened"
	case has(text, "established", "long-standing", "existing account"):
		return "established"
	default:
		return unknown
	}
}

func classifyTransactions(scenarioType, text string) string {
	switch strings.ToLower(scenarioType) {
	case "split_transaction":
		return "split_below_threshold"
	case "rapid_movement":
		return "rapid_passthrough"
	case "layering":
		return "multi_hop_layering"
	}
	switch {
	case has(text, "split", "structur", "smurf", "below threshold", "sub-threshold"):
		return "split_below_threshold"
	case has(text, "rapid", "pass-through", "passthrough", "in-and-out", "quick"):
		return "rapid_passthrough"
	case has(text, "layer", "multi-hop", "round trip", "circular"):
		return "multi_hop_layering"
	default:
		return unknown
	}
}

func classifyTemporal(text string) string {
	switch {
	case has(text, "cluster", "burst", "same day", "rapid succession"):
		return "clustered"
	case has(text, "spaced", "interval", "periodic", "gradually", "over weeks"):
		return "spaced"
	case has(text, "single", "one-off", "once"):
		return "single_event"
	default:
		return unknown
	}
}

func classifyNetwork(text string) string {
	switch {
	case has(text, "fan-in", "fan in", "many-to-one", "many to one", "concentrat"):
		return "fan_in"
	case has(text, "fan-out", "fan out", "one-to-many", "one to many", "distribut"):
		return "fan_out"
	case has(text, "multi-hop", "chain", "hop", "circular", "round trip"):
		return "multi_hop"
	case has(text, "counterparty", "peer", "direct"):
		return "simple"
	default:
		return unknown
	}
}

func classifyOwnership(text string) string {
	switch {
	case has(text, "shared controller", "common owner", "same owner", "common control", "beneficial owner"):
		return "shared_controller"
	case has(text, "independent", "unrelated", "no ownership"):
		return "independent"
	default:
		return unknown
	}
}

func classifyGeography(text string) string {
	switch {
	case has(text, "high-risk", "high risk", "sanction", "offshore", "tax haven", "fatf"):
		return "high_risk"
	case has(text, "cross-border", "cross border", "foreign", "overseas", "jurisdiction"):
		return "cross_border"
	case has(text, "domestic", "local", "same country"):
		return "domestic"
	default:
		return unknown
	}
}

func classifyBehavioralChange(text string) string {
	switch {
	case has(text, "dormant", "inactive", "reactivat"):
		return "dormancy_to_activity"
	case has(text, "velocity", "sudden increase", "spike", "change in volume"):
		return "velocity_shift"
	case has(text, "new counterpart", "expand", "widen"):
		return "counterparty_expansion"
	default:
		return "none"
	}
}

func classifyControlContext(text string) string {
	switch {
	case has(text, "structur", "smurf", "threshold", "below threshold", "sub-threshold"):
		return "structuring_v1"
	case has(text, "rapid", "pass-through", "passthrough", "in-and-out"):
		return "rapid_movement_v1"
	case has(text, "layer", "multi-hop", "round trip", "circular", "placement"):
		return "layering_v1"
	default:
		return "unmapped"
	}
}

// referenceTypologyID maps a scenario type to the typology it instantiates.
// This is a reference identifier only; it is not a claim of real-world validity.
func referenceTypology(s string) string {
	switch strings.ToLower(s) {
	case "split_transaction":
		return "typology.structuring.v1"
	case "rapid_movement":
		return "typology.rapid_movement.v1"
	case "layering":
		return "typology.layering.v1"
	default:
		return ""
	}
}

// countCounterparties counts distinct counterparty identifiers in the
// proposal's transaction pattern. The pattern is an open-schema map (allowed by
// AGENTS.md because Gemini output is not rigidly typed); only recognised keys
// are counted, and the fallback is the number of steps.
func countCounterparties(steps []scenario.TxPatternStep) int {
	seen := make(map[string]struct{}, len(steps))
	for _, step := range steps {
		for _, key := range []string{"counterparty", "counterparty_id", "creditor", "to_account"} {
			if v, ok := step[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					seen[s] = struct{}{}
				}
			}
		}
	}
	if len(seen) == 0 {
		return len(steps)
	}
	return len(seen)
}

// has reports whether any keyword is a substring of s. Callers pass lowercase
// keywords; s is already lowercased.
func has(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if kw != "" && strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// clamp01 bounds v to [0,1].
func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(0, math.Min(1, v))
}
