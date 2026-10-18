package discovery

import "math/rand"

// newRand returns a deterministic PRNG for the random strategy. A fixed seed
// makes a benchmark run reproducible, which the design document requires.
func newRand(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}
