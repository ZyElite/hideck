package driver

import "testing"

func TestIKEv2AlgToXFRMCrypt(t *testing.T) {
	cases := map[uint16]string{
		3:  "cbc(des3_ede)",
		12: "cbc(aes)",
		18: "rfc4106(gcm(aes))",
		20: "rfc4106(gcm(aes))",
		14: "rfc4309(ccm(aes))",
		11: "ecb(cipher_null)",
		99: "",
	}
	for in, want := range cases {
		if got := IKEv2AlgToXFRMCrypt(in); got != want {
			t.Errorf("IKEv2AlgToXFRMCrypt(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestIKEv2AlgToXFRMAuth(t *testing.T) {
	cases := map[uint16]string{
		1: "digest_null",
		2: "hmac(sha1)",
		5: "hmac(sha256)",
		6: "hmac(sha384)",
		7: "hmac(sha512)",
		9: "xcbc(aes)",
		99: "",
	}
	for in, want := range cases {
		if got := IKEv2AlgToXFRMAuth(in); got != want {
			t.Errorf("IKEv2AlgToXFRMAuth(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestIKEv2AlgToXFRMAead(t *testing.T) {
	if got := IKEv2AlgToXFRMAead(18); got != "rfc4106(gcm(aes))" {
		t.Errorf("AEAD(18) = %q", got)
	}
	if got := IKEv2AlgToXFRMAead(12); got != "" {
		t.Errorf("AEAD(12) = %q, want empty (CBC is not AEAD)", got)
	}
}

func TestNetToolError(t *testing.T) {
	e := &NetToolError{Op: "add route", Err: errUnsupportedPlatform}
	if e.Error() == "" {
		t.Error("Error() should be non-empty")
	}
	if e.Unwrap() != errUnsupportedPlatform {
		t.Error("Unwrap() should return the wrapped error")
	}
	var nilE *NetToolError
	if nilE.Error() != "" {
		t.Error("nil receiver should return empty string")
	}
}
