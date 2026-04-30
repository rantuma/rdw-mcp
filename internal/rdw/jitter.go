package rdw

import "math/rand/v2"

// pseudoJitter returns a uniformly distributed integer in [-rangeN, rangeN].
// Extracted into its own file so the cache logic in client.go is independent
// of any specific PRNG choice.
func pseudoJitter(rangeN int64) int64 {
	if rangeN <= 0 {
		return 0
	}

	// G404: math/rand/v2 is intentional for jitter; not security-sensitive.
	//nolint:gosec // jitter only; non-cryptographic randomness is fine
	v := rand.Int64N(rangeN*2 + 1)

	return v - rangeN
}
