package crypto

import (
	"bytes"
	"testing"
)

func TestSHA512TransformsUseRegisteredIKEv2IDs(t *testing.T) {
	prf := NewPRF(7)
	if prf == nil || prf.KeyLen() != 64 || prf.OutputSize() != 64 {
		t.Fatalf("PRF_HMAC_SHA2_512 = %#v", prf)
	}
	integrity := NewIntegrity(14)
	if integrity == nil || integrity.KeySize() != 64 || integrity.OutputSize() != 32 {
		t.Fatalf("AUTH_HMAC_SHA2_512_256 = %#v", integrity)
	}
	key := bytes.Repeat([]byte{0x11}, integrity.KeySize())
	if !integrity.Verify(key, []byte("payload"), integrity.Compute(key, []byte("payload"))) {
		t.Fatal("SHA-512 integrity self verification failed")
	}
	for _, legacyWrongID := range []uint16{6, 7, 8} {
		if NewIntegrity(legacyWrongID) != nil {
			t.Fatalf("unregistered integrity transform id %d unexpectedly accepted", legacyWrongID)
		}
	}
}
