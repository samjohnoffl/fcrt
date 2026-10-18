package genome

import "math"

// maxTransactionCount and maxCounterpartyCount are the magnitudes at which the
// numeric part of a dimension saturates. They are documented constants rather
// than learned parameters so the distance is reproducible and explainable.
const (
	maxTransactionCount  = 20.0
	maxCounterpartyCount = 10.0
)

// Distance returns the normalised [0,1] distance between two genomes.
// 0 means identical genomes; 1 means every dimension differs maximally.
//
// The distance is the mean of nine per-dimension distances. Categorical
// dimensions contribute 0 (same code) or 1 (different code). The Transactions
// and Network dimensions contribute the mean of their categorical mismatch and
// their normalised magnitude difference.
func Distance(a, b Genome) float64 {
	return DistanceComponents(a, b)["total"]
}

// DistanceComponents returns the per-dimension distances plus "total".
//
// Exposing the components is a requirement, not a convenience: the design
// document says a similarity or novelty score must show its inputs rather than
// presenting a single unexplained number.
func DistanceComponents(a, b Genome) map[string]float64 {
	da, db := a.Dimensions, b.Dimensions

	components := map[string]float64{
		DimActors:           categorical(da.Actors, db.Actors),
		DimAccounts:         categorical(da.Accounts, db.Accounts),
		DimTransactions:     mixed(da.Transactions, db.Transactions, da.TransactionCount, db.TransactionCount, maxTransactionCount),
		DimTemporal:         categorical(da.Temporal, db.Temporal),
		DimNetwork:          mixed(da.Network, db.Network, da.CounterpartyCount, db.CounterpartyCount, maxCounterpartyCount),
		DimOwnership:        categorical(da.Ownership, db.Ownership),
		DimGeography:        categorical(da.Geography, db.Geography),
		DimBehavioralChange: categorical(da.BehavioralChange, db.BehavioralChange),
		DimControlContext:   categorical(da.ControlContext, db.ControlContext),
	}

	total := 0.0
	for _, name := range DimensionNames {
		total += components[name]
	}
	components["total"] = clamp01(total / float64(len(DimensionNames)))
	return components
}

// Novelty returns the minimum distance from g to any prior genome.
//
// A genome with no priors is maximally novel (1.0). The result is a distance,
// not a probability: it says "how far is this candidate from the closest thing
// already tested", nothing more.
func Novelty(g Genome, priors []Genome) float64 {
	if len(priors) == 0 {
		return 1.0
	}
	min := 1.0
	for _, p := range priors {
		if d := Distance(g, p); d < min {
			min = d
		}
	}
	return clamp01(min)
}

// Nearest returns the prior genome closest to g and its distance.
// ok is false when priors is empty.
func Nearest(g Genome, priors []Genome) (nearest Genome, distance float64, ok bool) {
	if len(priors) == 0 {
		return Genome{}, 0, false
	}
	best := 1.0
	for _, p := range priors {
		if d := Distance(g, p); d < best {
			best = d
			nearest = p
		}
	}
	return nearest, best, true
}

// categorical returns 0 when two categorical codes match and 1 otherwise.
func categorical(a, b string) float64 {
	if a == b {
		return 0
	}
	return 1
}

// mixed combines a categorical mismatch with a normalised magnitude gap.
func mixed(codeA, codeB string, magA, magB int, scale float64) float64 {
	cat := categorical(codeA, codeB)
	if scale <= 0 {
		return cat
	}
	numeric := math.Abs(float64(magA)-float64(magB)) / scale
	return clamp01(0.5*cat + 0.5*clamp01(numeric))
}
