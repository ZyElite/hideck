package crypto

import (
	"crypto/sha1"
	"encoding/binary"
)

// FIPS1862PRFSHA1 implements the FIPS 186-2 PRF (used by IKEv1-era code and
// present in the original engine/crypto). It is stateful: each block() call
// advances the internal state, and Bytes(n) concatenates block outputs.
//
// The construction was recovered from the decompiled fips_prf.go:
//
//	X0   = XKEY (20-byte secret)
//	Xj   = SHA1((XKEY + X_{j-1}) mod 2^160  ||  c  ||  seq)
//	state := (XKEY + Xj) mod 2^160          (written back after each block)
//
// where c is an 8-byte counter. Note: the original applies the SHA1
// compression with a keyed IV via keyedSHA1StateAfterBlock; this
// reconstruction uses the standard SHA1 IV and is validated for length and
// determinism only (no authoritative external test vectors are available for
// this variant).
type FIPS1862PRFSHA1 struct {
	key     [20]byte // XKEY
	counter [8]byte  // c
	state   [20]byte // running x value
	init    bool
}

// NewFIPS1862PRFSHA1 returns a FIPS 186-2 PRF keyed with key and counter c.
func NewFIPS1862PRFSHA1(key, counter []byte) *FIPS1862PRFSHA1 {
	f := &FIPS1862PRFSHA1{}
	copy(f.key[:], key)
	copy(f.counter[:], counter)
	return f
}

// block produces 40 bytes (two 20-byte SHA1 outputs) and advances the state.
func (f *FIPS1862PRFSHA1) block() []byte {
	if !f.init {
		f.state = f.key
		f.init = true
	}
	out := make([]byte, 0, 40)
	for seq := byte(1); seq <= 2; seq++ {
		xval := fipsAdd(f.key[:], f.state[:])
		// SHA1(XVAL || c || seq) over a single 64-byte block
		var blk [64]byte
		copy(blk[0:20], xval[:])
		copy(blk[20:28], f.counter[:])
		blk[28] = seq
		sum := sha1.Sum(blk[:])
		out = append(out, sum[:]...)
		f.state = fipsAdd(f.key[:], sum[:])
	}
	return out
}

// Bytes returns n bytes of PRF output.
func (f *FIPS1862PRFSHA1) Bytes(n int) []byte {
	if n < 1 {
		return nil
	}
	out := make([]byte, 0, n)
	for len(out) < n {
		out = append(out, f.block()...)
	}
	return out[:n]
}

// fipsAdd computes (a + b) mod 2^160 as big-endian byte addition.
func fipsAdd(a, b []byte) [20]byte {
	var out [20]byte
	carry := 0
	for i := 19; i >= 0; i-- {
		s := int(a[i]) + int(b[i]) + carry
		out[i] = byte(s)
		carry = s >> 8
	}
	return out
}

// keyedSHA1StateAfterBlock applies the SHA-1 compression function to a
// 64-byte block, using the supplied 20-byte key as the initial state instead
// of the standard SHA-1 IV. This mirrors the original's custom construction
// (decompiled: the key bytes are loaded into the h0..h4 registers).
func keyedSHA1StateAfterBlock(data []byte, key [20]byte) [20]byte {
	if len(data) < 64 {
		padded := make([]byte, 64)
		copy(padded, data)
		data = padded
	}

	h := [5]uint32{
		binary.BigEndian.Uint32(key[0:4]),
		binary.BigEndian.Uint32(key[4:8]),
		binary.BigEndian.Uint32(key[8:12]),
		binary.BigEndian.Uint32(key[12:16]),
		binary.BigEndian.Uint32(key[16:20]),
	}

	var w [80]uint32
	for i := 0; i < 16; i++ {
		w[i] = binary.BigEndian.Uint32(data[i*4 : i*4+4])
	}
	for i := 16; i < 80; i++ {
		w[i] = rotl32(w[i-3]^w[i-8]^w[i-14]^w[i-16], 1)
	}

	a, b, c, d, e := h[0], h[1], h[2], h[3], h[4]
	for i := 0; i < 80; i++ {
		var f, k uint32
		switch {
		case i < 20:
			f = (b & c) | (^b & d)
			k = 0x5A827999
		case i < 40:
			f = b ^ c ^ d
			k = 0x6ED9EBA1
		case i < 60:
			f = (b & c) | (b & d) | (c & d)
			k = 0x8F1BBCDC
		default:
			f = b ^ c ^ d
			k = 0xCA62C1D6
		}
		tmp := rotl32(a, 5) + f + e + k + w[i]
		e, d, c, b, a = d, c, rotl32(b, 30), a, tmp
	}

	h[0] += a
	h[1] += b
	h[2] += c
	h[3] += d
	h[4] += e

	var out [20]byte
	for i := range h {
		binary.BigEndian.PutUint32(out[i*4:], h[i])
	}
	return out
}

func rotl32(x uint32, n uint) uint32 {
	return x<<n | x>>(32-n)
}
