package genome

import "math"

// Distance computes the Euclidean (L2) distance between two genomes.
//
// Returns a value in [0,1] where 0 = identical, 1 = completely different.
// This is used for novelty N(x) in the adaptive score S(x) = wc·C + wn·N + wi·I + wv·V.
func Distance(a, b Genome) float64 {
	var sum float64
	for i := range a {
		d := a[i] - b[i]
		sum += d * d
	}
	return math.Sqrt(sum)
}
