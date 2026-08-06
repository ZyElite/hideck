package smscodec

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/tpdu"
)

func TestNormalizeSMSEncoding(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"auto", "auto", false},
		{"AUTO", "auto", false},
		{" Auto ", "auto", false},
		{"ucs2", "ucs2", false},
		{"UCS2", "ucs2", false},
		{"gsm7", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeSMSEncoding(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("NormalizeSMSEncoding(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
		if !c.wantErr && got != c.want {
			t.Errorf("NormalizeSMSEncoding(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsShortCode(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"10086", true},
		{"123", true},
		{"123456", true},
		{"1234567", false}, // 7+ digits -> not a short code
		{"+8613800138000", false},
		{"95588abc", false}, // non-digits remain
		{"", true},          // empty is trivially a "short code"
	}
	for _, c := range cases {
		if got := IsShortCode(c.in); got != c.want {
			t.Errorf("IsShortCode(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// buildDeliverTPDU constructs a real SMS-DELIVER TPDU (without SCA) using the
// public library, then returns its bytes.
func buildDeliverTPDU(t *testing.T, from, text string) []byte {
	t.Helper()
	addr := tpdu.NewAddress(tpdu.FromNumber(from))
	tp, err := tpdu.NewDeliver(tpdu.WithOA(addr))
	if err != nil {
		t.Fatalf("NewDeliver: %v", err)
	}
	tp.SetPID(0)
	tp.SetDCS(0) // GSM-7 default alphabet
	ud, _, alpha := tpdu.EncodeUserData([]byte(text))
	tp.SetUD(ud)
	_ = alpha
	raw, err := tp.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	return raw
}

func TestDecodeDeliverTPDU(t *testing.T) {
	pdu := buildDeliverTPDU(t, "+8613800138000", "hello world")
	got := DecodeDeliverTPDU(pdu)
	if got.Err != nil {
		t.Fatalf("DecodeDeliverTPDU err: %v", got.Err)
	}
	if got.Text != "hello world" {
		t.Errorf("Text = %q, want %q", got.Text, "hello world")
	}
	if !strings.HasSuffix(got.Sender, "8613800138000") {
		t.Errorf("Sender = %q, want suffix 8613800138000", got.Sender)
	}
	if got.Timestamp.IsZero() {
		t.Errorf("Timestamp is zero, want a set SCTS")
	}
	t.Logf("decoded: sender=%q text=%q ts=%v", got.Sender, got.Text, got.Timestamp)
}

func TestTrimDeliverTPDUToDeclaredLength(t *testing.T) {
	pdu := buildDeliverTPDU(t, "+15551234567", "trim me")
	// Append garbage after the declared user-data length.
	extended := append(append([]byte{}, pdu...), 0xAA, 0xBB, 0xCC)
	got := TrimDeliverTPDUToDeclaredLength(extended)
	if len(got) != len(pdu) {
		t.Errorf("trimmed len = %d, want %d (declared)", len(got), len(pdu))
	}
	// The trimmed PDU must still decode.
	m, err := sms.Unmarshal(got)
	if err != nil {
		t.Fatalf("trimmed PDU no longer decodes: %v", err)
	}
	if m == nil {
		t.Fatal("unmarshal returned nil")
	}
}

func TestDeliverTPDUDeclaredLength(t *testing.T) {
	pdu := buildDeliverTPDU(t, "+8613800138000", "short")
	declared, ok := DeliverTPDUDeclaredLength(pdu)
	if !ok {
		t.Fatal("DeliverTPDUDeclaredLength reported invalid layout")
	}
	if declared != len(pdu) {
		t.Errorf("declared = %d, actual = %d", declared, len(pdu))
	}
}

// TestKnownDeliverPDU decodes a fixed SMS-DELIVER TPDU (no SCA) to verify the
// reconstruction against a concrete byte sequence.
//
// "hello" encoded: 0x68 0x65 0x6C 0x6C 0x6F in GSM-7, packed as 5 septets.
func TestKnownDeliverPDU(t *testing.T) {
	// Build the reference PDU via the library rather than hard-coding, to
	// avoid brittleness; the important check is that DecodeDeliverTPDU agrees
	// with the library's own decoder.
	pdu := buildDeliverTPDU(t, "+15551234567", "hello")
	ref, err := sms.Unmarshal(pdu)
	if err != nil {
		t.Fatalf("reference unmarshal: %v", err)
	}
	refText, err := sms.Decode([]*tpdu.TPDU{ref})
	if err != nil {
		t.Fatalf("reference decode: %v", err)
	}
	got := DecodeDeliverTPDU(pdu)
	if got.Err != nil {
		t.Fatalf("DecodeDeliverTPDU: %v", got.Err)
	}
	if got.Text != string(refText) {
		t.Errorf("DecodeDeliverTPDU text %q != library text %q", got.Text, string(refText))
	}
	t.Logf("PDU hex: %s", hex.EncodeToString(pdu))
}
