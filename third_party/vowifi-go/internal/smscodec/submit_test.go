package smscodec

import (
	"strings"
	"testing"

	"github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/tpdu"
)

func TestBuildSubmitTPDUsPreservesTextAndDestination(t *testing.T) {
	parts, err := BuildSubmitTPDUObjectsWithOptions("+447700900123", " hello ", SubmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || parts[0].DA.Number() != "+447700900123" {
		t.Fatalf("parts = %+v", parts)
	}
	decoded, err := sms.Decode([]*tpdu.TPDU{&parts[0]})
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != " hello " {
		t.Fatalf("decoded text = %q", decoded)
	}
}

func TestBuildSubmitTPDUsUsesRealUCS2AndShortCodeTON(t *testing.T) {
	parts, err := BuildSubmitTPDUObjectsWithOptions("10086", "你好", SubmitOptions{Encoding: "ucs2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 {
		t.Fatalf("parts = %d", len(parts))
	}
	if parts[0].DCS != tpdu.DcsUCS2Data {
		t.Fatalf("DCS = 0x%02x", byte(parts[0].DCS))
	}
	if parts[0].DA.TypeOfNumber() != tpdu.TonUnknown || parts[0].DA.NumberingPlan() != tpdu.NpISDN {
		t.Fatalf("short-code address = %+v", parts[0].DA)
	}
	decoded, err := sms.Decode([]*tpdu.TPDU{&parts[0]})
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != "你好" {
		t.Fatalf("decoded text = %q", decoded)
	}
}

func TestBuildSubmitTPDUsUsesProvidedConcatReference(t *testing.T) {
	parts, err := BuildSubmitTPDUObjectsWithOptions(
		"+447700900123", strings.Repeat("multipart ", 40),
		SubmitOptions{ConcatReference: 37},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) < 2 {
		t.Fatalf("parts = %d", len(parts))
	}
	for index := range parts {
		total, sequence, reference, ok := parts[index].ConcatInfo()
		if !ok || total != len(parts) || sequence != index+1 || reference != 37 {
			t.Fatalf("part %d concat=(%d,%d,%d,%v)", index+1, total, sequence, reference, ok)
		}
	}
}

func TestSetSubmitMessageReference(t *testing.T) {
	encoded, _, err := BuildSubmitTPDUs("85075", "INFO")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := SetSubmitMessageReference(encoded[0], 0x5a)
	if err != nil {
		t.Fatal(err)
	}
	message := tpdu.TPDU{Direction: tpdu.MO}
	if err := message.UnmarshalBinary(updated); err != nil {
		t.Fatal(err)
	}
	if message.MR != 0x5a || message.SmsType() != tpdu.SmsSubmit {
		t.Fatalf("message = %+v", message)
	}
	if _, err := SetSubmitMessageReference([]byte{0xff}, 1); err == nil {
		t.Fatal("malformed TPDU did not fail")
	}
}
