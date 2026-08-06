package crypto

// Wipe zeroes b so sensitive key material does not linger in memory.
// (Best-effort: the compiler may optimise the write away; the original used
// the same approach.)
func Wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
