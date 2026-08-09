package smscodec

import (
	"strings"
	"testing"

	"github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/tpdu"
)

func TestGSM7ExtensionAndMultipartRoundTrip(t *testing.T) {
	text := strings.Repeat("GSM ^{}\\[~]| euro: € ", 12)
	encoded, lengths, err := BuildSubmitTPDUsWithOptions("85075", text, SubmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) < 2 || len(lengths) != len(encoded) {
		t.Fatalf("parts=%d lengths=%d", len(encoded), len(lengths))
	}
	parts := decodeSubmitParts(t, encoded)
	decoded, err := sms.Decode(parts)
	if err != nil || string(decoded) != text {
		t.Fatalf("sms.Decode() = (%q, %v)", decoded, err)
	}
	for index, part := range parts {
		if lengths[index] != len(encoded[index]) || part.DA.TypeOfNumber() != tpdu.TonUnknown {
			t.Fatalf("part %d length/TON mismatch", index+1)
		}
	}
}

func TestUCS2MultipartRoundTrip(t *testing.T) {
	text := strings.Repeat("你好，世界。", 20)
	encoded, _, err := BuildSubmitTPDUsWithOptions("+447700900123", text, SubmitOptions{Encoding: SMSEncodingUCS2})
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) < 2 {
		t.Fatalf("parts=%d, want multipart", len(encoded))
	}
	parts := decodeSubmitParts(t, encoded)
	decoded, err := sms.Decode(parts)
	if err != nil || string(decoded) != text {
		t.Fatalf("sms.Decode() = (%q, %v)", decoded, err)
	}
	for _, part := range parts {
		if part.DCS != tpdu.DcsUCS2Data {
			t.Fatalf("DCS=0x%02x", byte(part.DCS))
		}
	}
}

func decodeSubmitParts(t *testing.T, encoded [][]byte) []*tpdu.TPDU {
	t.Helper()
	parts := make([]*tpdu.TPDU, len(encoded))
	for index := range encoded {
		parts[index] = &tpdu.TPDU{Direction: tpdu.MO}
		if err := parts[index].UnmarshalBinary(encoded[index]); err != nil {
			t.Fatal(err)
		}
	}
	return parts
}
