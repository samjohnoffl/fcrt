package genome

import (
	"math"

	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

// Genome is a fixed-width 5-dimensional encoding of a scenario.Proposal.
// All values are normalized to [0,1] by the Encode function.
type Genome [5]float64

// GenomeVersion is the schema version for forward/backward compatibility.
const GenomeVersion = "1.0"

// Encode populates g from a scenario.Proposal. Must be called before Distance.
// All five dimensions are normalized to [0,1] so Distance is comparable.
//
// Dimension 3 (selection count) is left at 0 by Encode because it is external
// metadata, not intrinsic candidate DNA. Use SetSelectionCount to populate it.
func Encode(g *Genome, p scenario.Proposal) {
	g[0] = ruleFocusWeight(p.ExpectedEvasionVector)
	g[1] = float64(scenarioTypeIndex(p.ScenarioType)) / 3.0 // 4 types -> [0,1]
	g[2] = evasionHashBucket(p.ExpectedEvasionVector) / 7.0 // 8 buckets -> [0,1]
	g[3] = 0.0                                              // selection count: set externally
	g[4] = coverageGap(p.ExpectedEvasionVector)
}

// SetSelectionCount records how many times a candidate has been selected,
// normalized against maxCount into [0,1]. This dimension is external metadata
// and is intentionally excluded from intrinsic duplicate detection.
func (g *Genome) SetSelectionCount(count, maxCount int) {
	if maxCount <= 0 || count <= 0 {
		g[3] = 0
		return
	}
	g[3] = math.Min(1.0, float64(count)/float64(maxCount))
}

// Decode reads genome dimensions back into human-readable labels.
func Decode(g Genome) (ruleFocus string, scenarioType string, evasionBucket int, novelty int, coverageGap float64) {
	ruleFocus = ruleLabel(g[0])
	idx := int(g[1] * 3.0)
	if idx < 0 {
		idx = 0
	}
	if idx > 3 {
		idx = 3
	}
	scenarioType = scenarioLabels[idx]
	evasionBucket = int(g[2] * 7.0)
	novelty = int(g[3] * 100.0)
	coverageGap = g[4]
	return
}

// ruleFocusWeight maps an evasion vector to a [0,1] priority weight.
// Higher = candidate targets specific AML rules (structuring, layering, etc.).
func ruleFocusWeight(ev string) float64 {
	if ev == "" {
		return 0.0
	}
	lower := lowerASCII(ev)
	switch {
	case containsAnyKW(lower, "structur", "smurf", "layer", "round.trip", "shell"):
		return 1.0
	case containsAnyKW(lower, "rapid", "quick", "fast", "mule"):
		return 0.8
	case containsAnyKW(lower, "split", "integration", "mixing"):
		return 0.7
	default:
		return 0.5
	}
}

// scenarioTypeIndex maps a scenario type string to a fixed index in [0,3].
var scenarioTypeIndexMap = map[string]int{
	"split_transaction": 0,
	"rapid_movement":    1,
	"layering":          2,
	"other":             3,
}

func scenarioTypeIndex(t string) int {
	if idx, ok := scenarioTypeIndexMap[t]; ok {
		return idx
	}
	return 3 // "other"
}

// evasionHashBucket hashes an evasion vector into a bucket [0,7] for similarity grouping.
func evasionHashBucket(ev string) float64 {
	if ev == "" {
		return 0.0
	}
	var hash int
	for _, r := range ev {
		hash ^= int(r) * 31
		hash ^= hash >> 16
	}
	return float64((hash & 0x7FF) % 8)
}

// coverageGap = 1 - coverage_weight. Higher gap = less-covered rules = higher priority.
func coverageGap(ev string) float64 {
	return 1.0 - ruleFocusWeight(ev)
}

// lowerASCII lowercases ASCII-only strings.
func lowerASCII(s string) string {
	runes := []rune(s)
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			runes[i] = r + 32
		}
	}
	return string(runes)
}

// containsAnyKW checks if any of the given keyword strings are present as substrings.
func containsAnyKW(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if len(kw) == 0 || len(s) < len(kw) {
			continue
		}
		match := true
		for i := 0; i < len(kw); i++ {
			if s[i] != kw[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// ruleLabel returns a human-readable label for a rule focus weight.
func ruleLabel(w float64) string {
	if w >= 0.8 {
		return "high"
	}
	if w >= 0.5 {
		return "medium"
	}
	return "low"
}

var scenarioLabels = [4]string{"split_tx", "rapid_movement", "layering", "other"}
