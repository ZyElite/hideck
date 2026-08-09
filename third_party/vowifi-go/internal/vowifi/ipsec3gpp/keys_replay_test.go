package ipsec3gpp

import (
	"bytes"
	"testing"
)

func TestDeriveSecureChannelKeys(t *testing.T) {
	ck := bytes.Repeat([]byte{0x11}, 16)
	ik := bytes.Repeat([]byte{0x22}, 16)
	authKey, encKey, err := DeriveSecureChannelKeys(ck, ik, " HMAC(SHA1) ", "CBC(AES)")
	if err != nil {
		t.Fatalf("DeriveSecureChannelKeys: %v", err)
	}
	if len(authKey) != 20 || !bytes.Equal(authKey[:16], ik) || !bytes.Equal(authKey[16:], make([]byte, 4)) {
		t.Fatalf("invalid SHA-1 authentication key: %x", authKey)
	}
	if !bytes.Equal(encKey, ck) {
		t.Fatalf("invalid AES encryption key: %x", encKey)
	}
	authKey, encKey, err = DeriveSecureChannelKeys(ck, ik, "hmac-md5-96", "cipher_null")
	if err != nil || !bytes.Equal(authKey, ik) || encKey != nil {
		t.Fatalf("null channel keys = %x/%x, %v", authKey, encKey, err)
	}
}

func TestDeriveSecureChannelKeyErrors(t *testing.T) {
	tests := []struct {
		name, auth, enc, want string
		ck, ik                []byte
	}{
		{name: "short md5", auth: "hmac(md5)", enc: "null", ik: make([]byte, 15), want: "ipsec3gpp: IK too short for HMAC-MD5-96"},
		{name: "short sha1", auth: "hmac(sha1)", enc: "null", ik: make([]byte, 15), want: "ipsec3gpp: IK too short for HMAC-SHA-1-96"},
		{name: "short aes", auth: "hmac(sha1)", enc: "aes-cbc", ik: make([]byte, 16), ck: make([]byte, 15), want: "ipsec3gpp: CK too short for AES-CBC"},
		{name: "bad auth", auth: "sha256", enc: "null", want: `ipsec3gpp: unsupported authentication algorithm "sha256"`},
		{name: "bad enc", auth: "hmac(sha1)", enc: "rot13", ik: make([]byte, 16), want: `ipsec3gpp: unsupported encryption algorithm "rot13"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := DeriveSecureChannelKeys(test.ck, test.ik, test.auth, test.enc)
			if err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDerive3DESKeyFromCK(t *testing.T) {
	key, err := Derive3DESKeyFromCK(bytes.Repeat([]byte{0x10}, 16))
	if err != nil {
		t.Fatalf("Derive3DESKeyFromCK: %v", err)
	}
	if len(key) != 24 || !bytes.Equal(key[16:], key[:8]) {
		t.Fatalf("invalid 3DES key: %x", key)
	}
	for index, value := range key {
		if byteOnes(value)%2 == 0 {
			t.Fatalf("key byte %d does not have odd parity: %02x", index, value)
		}
	}
	if _, err := Derive3DESKeyFromCK(make([]byte, 15)); err == nil || err.Error() != "ipsec3gpp: CK too short for 3DES" {
		t.Fatalf("short CK error = %v", err)
	}
}

func byteOnes(value byte) int {
	count := 0
	for value != 0 {
		count += int(value & 1)
		value >>= 1
	}
	return count
}

func TestReplayWindowStats(t *testing.T) {
	window := NewReplayWindow(32)
	for _, sequence := range []uint32{1, 2, 100, 99} {
		if !window.Accept(sequence) {
			t.Fatalf("sequence %d was unexpectedly rejected", sequence)
		}
	}
	for _, sequence := range []uint32{0, 1, 99} {
		if window.Accept(sequence) {
			t.Fatalf("sequence %d was unexpectedly accepted", sequence)
		}
	}
	if got, want := window.Snapshot(), (ReplayStats{Accepted: 4, Duplicate: 1, TooOld: 2}); got != want {
		t.Fatalf("stats = %+v, want %+v", got, want)
	}
}

func TestNewPolicyClonesAllKeyMaterial(t *testing.T) {
	policy := testPolicy(nil, nil, EncryptionAES)
	cloned, err := NewPolicy(policy)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	policy.LocalIP[0] ^= 0xff
	policy.FlowC.CK[0] = 0x44
	policy.FlowC.IK[0] = 0x55
	if cloned.LocalIP[0] == policy.LocalIP[0] || cloned.FlowC.CK[0] == policy.FlowC.CK[0] ||
		cloned.FlowC.IK[0] == policy.FlowC.IK[0] || cloned.FlowS.CK[0] == policy.FlowS.CK[0] || cloned.FlowS.IK[0] == policy.FlowS.IK[0] {
		t.Fatal("NewPolicy retained caller-owned slices")
	}
}
