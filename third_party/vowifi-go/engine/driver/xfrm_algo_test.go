package driver

import (
	"testing"
)

func TestIKEv2AlgToXFRMCrypt(t *testing.T) {
	tests := []struct {
		id, keyBits uint16
		wantName    string
		wantBits    int
	}{
		{id: 2, wantName: "cbc(des)", wantBits: 64},
		{id: 3, wantName: "cbc(des3_ede)", wantBits: 192},
		{id: 12, wantName: "cbc(aes)", wantBits: 128},
		{id: 12, keyBits: 256, wantName: "cbc(aes)", wantBits: 256},
		{id: 13, keyBits: 192, wantName: "rfc3686(ctr(aes))", wantBits: 192},
	}
	for _, test := range tests {
		result, err := IKEv2AlgToXFRMCrypt(test.id, int(test.keyBits))
		if err != nil {
			t.Fatalf("crypt %d: %v", test.id, err)
		}
		if result.Name != test.wantName || result.KeyBits != test.wantBits {
			t.Errorf("crypt %d = %+v, want %s/%d", test.id, result, test.wantName, test.wantBits)
		}
	}
	if _, err := IKEv2AlgToXFRMCrypt(99, 0); err == nil {
		t.Fatal("unsupported encryption algorithm was accepted")
	}
}

func TestIKEv2AlgToXFRMAuth(t *testing.T) {
	tests := []struct {
		id                      uint16
		name                    string
		keyBits, truncationBits int
	}{
		{1, "hmac(md5)", 128, 96},
		{2, "hmac(sha1)", 160, 96},
		{12, "hmac(sha256)", 256, 128},
		{13, "hmac(sha384)", 384, 192},
		{14, "hmac(sha512)", 512, 256},
	}
	for _, test := range tests {
		result, err := IKEv2AlgToXFRMAuth(test.id)
		if err != nil {
			t.Fatalf("auth %d: %v", test.id, err)
		}
		if result.Name != test.name || result.KeyBits != test.keyBits || result.TruncateBits != test.truncationBits {
			t.Errorf("auth %d = %+v", test.id, result)
		}
	}
	if _, err := IKEv2AlgToXFRMAuth(99); err == nil {
		t.Fatal("unsupported integrity algorithm was accepted")
	}
}

func TestIKEv2AlgToXFRMAead(t *testing.T) {
	tests := []struct {
		id               uint16
		name             string
		keyBits, icvBits int
	}{
		{18, "rfc4106(gcm(aes))", 160, 64},
		{19, "rfc4106(gcm(aes))", 160, 96},
		{20, "rfc4106(gcm(aes))", 160, 128},
		{14, "rfc4309(ccm(aes))", 152, 64},
		{15, "rfc4309(ccm(aes))", 152, 96},
		{16, "rfc4309(ccm(aes))", 152, 128},
	}
	for _, test := range tests {
		result, err := IKEv2AlgToXFRMAead(test.id, 0)
		if err != nil {
			t.Fatalf("AEAD %d: %v", test.id, err)
		}
		if result.Name != test.name || result.KeyBits != test.keyBits || result.ICVBits != test.icvBits {
			t.Errorf("AEAD %d = %+v", test.id, result)
		}
		if !IsAEADAlgorithm(test.id) {
			t.Errorf("IsAEADAlgorithm(%d) = false", test.id)
		}
	}
	if IsAEADAlgorithm(12) {
		t.Fatal("AES-CBC classified as AEAD")
	}
	if _, err := IKEv2AlgToXFRMAead(12, 0); err == nil {
		t.Fatal("non-AEAD algorithm was accepted")
	}
}
