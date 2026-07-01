package interp

// prng is a small deterministic pseudo-random generator (xorshift128+) used by
// Math.random. Each interpreter owns its own instance so that seeding or
// consuming randomness in one interpreter never perturbs another — mirroring
// golua's per-VM deterministic math.random.
type prng struct {
	s0, s1 uint64
}

// newPRNG seeds a generator. The seed is fixed so runs are reproducible; an
// embedder that wants nondeterminism can reseed via SetRandomSeed.
func newPRNG(seed uint64) *prng {
	if seed == 0 {
		seed = 0x9E3779B97F4A7C15
	}
	// SplitMix64 to spread the seed across both state words.
	next := func() uint64 {
		seed += 0x9E3779B97F4A7C15
		z := seed
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		return z ^ (z >> 31)
	}
	return &prng{s0: next(), s1: next()}
}

// next returns a float64 in [0, 1).
func (p *prng) next() float64 {
	s1 := p.s0
	s0 := p.s1
	p.s0 = s0
	s1 ^= s1 << 23
	s1 ^= s1 >> 17
	s1 ^= s0
	s1 ^= s0 >> 26
	p.s1 = s1
	sum := p.s0 + p.s1
	// Take the top 53 bits for a double in [0,1).
	return float64(sum>>11) / float64(uint64(1)<<53)
}

// SetRandomSeed reseeds the interpreter's Math.random generator.
func (i *Interpreter) SetRandomSeed(seed uint64) { i.rng = newPRNG(seed) }
