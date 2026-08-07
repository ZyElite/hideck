package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/des"
	"crypto/sha1"
	"encoding/hex"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex: %v", err)
	}
	return b
}

// TestHmacSHA1_96 uses the RFC 2202 test vector (key = 20 bytes of 0x0b).
func TestHmacSHA1_96(t *testing.T) {
	key := mustHex(t, "0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b")
	data := []byte("Hi There")
	want := mustHex(t, "b617318655057264e28bc0b6fb3c8e3657ae75a5")[:12]

	alg := newHmacSHA1_96()
	if alg.KeySize() != 20 || alg.OutputSize() != 12 {
		t.Fatalf("key/out size = %d/%d", alg.KeySize(), alg.OutputSize())
	}
	if got := alg.Compute(key, data); !bytes.Equal(got, want) {
		t.Errorf("HMAC-SHA1-96 = %x, want %x", got, want)
	}
	if !alg.Verify(key, data, want) {
		t.Error("Verify failed for correct MAC")
	}
}

// TestHmacSHA256_128 sanity checks truncation to 128 bits.
func TestHmacSHA256_128(t *testing.T) {
	alg := newHmacSHA256_128()
	got := alg.Compute(bytes.Repeat([]byte{0x0b}, 32), []byte("Hi There"))
	if len(got) != 16 {
		t.Fatalf("output len = %d, want 16", len(got))
	}
	if !alg.Verify(bytes.Repeat([]byte{0x0b}, 32), []byte("Hi There"), got) {
		t.Error("Verify failed")
	}
}

// TestAESXCBCMAC uses the RFC 3566 test vectors (cases 1, 5 and 6).
func TestAESXCBCMAC(t *testing.T) {
	key := mustHex(t, "000102030405060708090a0b0c0d0e0f")
	cases := []struct {
		msg  string
		want string
	}{
		{"", "75f0251d528ac01c4573dfd584d79f29"}, // case 1: empty
		{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f", "f54f0ec8d2b9f3d36807734bd5283fd4"},     // case 5: 32 bytes
		{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f2021", "becbb3bccdb518a30677d5481fb6b4d8"}, // case 6: 34 bytes
	}
	for i, c := range cases {
		msg := mustHex(t, c.msg)
		want := mustHex(t, c.want)
		got := aesXCBCMAC(key, msg)
		if !bytes.Equal(got, want) {
			t.Errorf("case %d: XCBC-MAC = %x, want %x", i+1, got, want)
		}
	}
	// aesXCBC96 truncates to 12 bytes.
	msg := mustHex(t, "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	want96 := mustHex(t, "f54f0ec8d2b9f3d36807734b")
	alg := newAesXCBC96()
	if !alg.Verify(key, msg, want96) {
		t.Error("aesXCBC96 Verify failed")
	}
}

// TestPrfPlus uses the RFC 7296 §2.13 example (HMAC-SHA1 based PRF).
func TestPrfPlus(t *testing.T) {
	key := bytes.Repeat([]byte{0xaa}, 20)
	seed := []byte("seed")
	out := PrfPlus(&hmacPRF{newHash: sha1.New, keyLen: 20}, key, seed, 200)
	if len(out) != 200 {
		t.Fatalf("PrfPlus length = %d, want 200", len(out))
	}
	// Verify the structure: T1 = PRF(K, seed || 0x01)
	t1 := computeHMAC(sha1.New, key, append(append([]byte{}, seed...), 0x01))
	if !bytes.Equal(out[:20], t1) {
		t.Error("T1 mismatch")
	}
	// T2 = PRF(K, T1 || seed || 0x02)
	t2input := append(append([]byte{}, t1...), seed...)
	t2input = append(t2input, 0x02)
	t2 := computeHMAC(sha1.New, key, t2input)
	if !bytes.Equal(out[20:40], t2) {
		t.Error("T2 mismatch")
	}
}

// TestDiffieHellman verifies both sides compute the same shared secret.
func TestDiffieHellman(t *testing.T) {
	a, err := NewDiffieHellman(14)
	if err != nil {
		t.Fatalf("NewDiffieHellman(14): %v", err)
	}
	b, err := NewDiffieHellman(14)
	if err != nil {
		t.Fatalf("NewDiffieHellman(14): %v", err)
	}
	if err := a.GenerateKey(); err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if err := b.GenerateKey(); err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sa, err := a.ComputeSharedSecret(b.PublicKeyBytes())
	if err != nil {
		t.Fatalf("a secret: %v", err)
	}
	sb, err := b.ComputeSharedSecret(a.PublicKeyBytes())
	if err != nil {
		t.Fatalf("b secret: %v", err)
	}
	if !bytes.Equal(sa, sb) {
		t.Error("shared secrets differ")
	}
	if len(sa) != 256 { // MODP-2048 => 256 bytes
		t.Errorf("secret length = %d, want 256", len(sa))
	}
}

// TestAESCBCRoundTrip checks encrypt/decrypt symmetry. AES-CBC is raw (no
// padding): the input must already be a block multiple, matching how the
// IKEv2/ESP layers apply their own padding.
func TestAESCBCRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, 16)
	c, err := PrepareCipher(EncrAESCBC, key)
	if err != nil {
		t.Fatalf("PrepareCipher: %v", err)
	}
	iv := bytes.Repeat([]byte{0x22}, 16)
	pt := []byte("hello vohive voip") // 17 bytes + 15 zero bytes = 32
	pt = append(pt, make([]byte, 15)...)
	if len(pt)%aes.BlockSize != 0 {
		t.Fatalf("test input not block aligned: %d", len(pt))
	}
	ct := c.Seal(nil, pt, iv, nil)
	back, err := c.Open(nil, ct, iv, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(back, pt) {
		t.Errorf("round trip = %q, want %q", back, pt)
	}
}

func TestPrepared3DESCipherRoundTrip(t *testing.T) {
	cipher, err := PrepareCipher(Encr3DESCBC, bytes.Repeat([]byte{0x31}, 24))
	if err != nil {
		t.Fatalf("PrepareCipher: %v", err)
	}
	plain := bytes.Repeat([]byte{0x42}, 2*des.BlockSize)
	iv := bytes.Repeat([]byte{0x53}, des.BlockSize)
	encrypted := cipher.Seal(nil, plain, iv, nil)
	decrypted, err := cipher.Open(nil, encrypted, iv, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(decrypted, plain) {
		t.Fatalf("3DES round trip = %x, want %x", decrypted, plain)
	}
}

// TestAESGCMRoundTrip checks AES-GCM encrypt/decrypt symmetry with the
// RFC 4106 key format (K|salt) and the 8-byte packet IV.
func TestAESGCMRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x33}, 20) // 16-byte AES key + 4-byte salt
	c, err := PrepareCipher(EncrAESGCM16, key)
	if err != nil {
		t.Fatalf("PrepareCipher: %v", err)
	}
	if c.IVSize() != 8 || c.BlockSize() != 16 {
		t.Fatalf("IVSize/BlockSize = %d/%d, want 8/16", c.IVSize(), c.BlockSize())
	}
	iv := bytes.Repeat([]byte{0x44}, c.IVSize())
	pt := []byte("voice over wifi payload")
	aad := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0x01, 0x02, 0x03, 0x04}
	ct := c.Seal(nil, pt, iv, aad)
	if len(ct) != len(pt)+16 {
		t.Errorf("ciphertext length = %d, want %d", len(ct), len(pt)+16)
	}
	back, err := c.Open(nil, ct, iv, aad)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(back, pt) {
		t.Errorf("round trip = %q, want %q", back, pt)
	}
	// AAD mismatch must be rejected.
	if _, err := c.Open(nil, ct, iv, []byte("bad-aad!")); err == nil {
		t.Error("Open accepted wrong AAD")
	}
	// GCM is authenticated: a flipped tag byte must fail.
	bad := append([]byte{}, ct...)
	bad[len(bad)-1] ^= 0x01
	if _, err := c.Open(nil, bad, iv, aad); err == nil {
		t.Error("Open accepted tampered ciphertext")
	}
}

// TestLegacyCipher round-trips through the legacy Cipher interface for the
// supported transforms.
func TestLegacyCipher(t *testing.T) {
	pt := []byte("legacy cipher round trip")
	cases := []struct {
		id  uint16
		key []byte
		iv  []byte
	}{
		{EncrAESCBC, bytes.Repeat([]byte{0x11}, 16), bytes.Repeat([]byte{0x55}, 16)},
		{Encr3DESCBC, bytes.Repeat([]byte{0x22}, 24), bytes.Repeat([]byte{0x55}, 8)},
		{EncrAESGCM16, bytes.Repeat([]byte{0x33}, 20), bytes.Repeat([]byte{0x44}, 8)},
		{EncrNull, nil, nil},
	}
	for _, c := range cases {
		ci, err := NewCipher(c.id, c.key)
		if err != nil {
			t.Errorf("NewCipher(%d): %v", c.id, err)
			continue
		}
		ct := ci.Encrypt(nil, c.iv, pt)
		back, err := ci.Decrypt(nil, c.iv, ct)
		if err != nil {
			t.Errorf("Decrypt(%d): %v", c.id, err)
			continue
		}
		if !bytes.Equal(back, pt) {
			t.Errorf("round trip (%d) = %q, want %q", c.id, back, pt)
		}
	}
}

// TestFIPS1862PRF checks determinism and output length.
func TestFIPS1862PRF(t *testing.T) {
	f := NewFIPS1862PRFSHA1(bytes.Repeat([]byte{0xab}, 20), []byte{0, 0, 0, 0, 0, 0, 0, 1})
	o1 := f.Bytes(64)
	if len(o1) != 64 {
		t.Fatalf("FIPS1862PRF length = %d, want 64", len(o1))
	}
	// A fresh instance with the same key/counter must produce the same output.
	f2 := NewFIPS1862PRFSHA1(bytes.Repeat([]byte{0xab}, 20), []byte{0, 0, 0, 0, 0, 0, 0, 1})
	o2 := f2.Bytes(64)
	if !bytes.Equal(o1, o2) {
		t.Error("FIPS1862PRF not deterministic")
	}
	// Different counter must produce different output.
	f3 := NewFIPS1862PRFSHA1(bytes.Repeat([]byte{0xab}, 20), []byte{0, 0, 0, 0, 0, 0, 0, 2})
	o3 := f3.Bytes(64)
	if bytes.Equal(o1, o3) {
		t.Error("FIPS1862PRF ignores the counter")
	}
}
