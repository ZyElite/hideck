package smscodec

import (
	"testing"

	"github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/tpdu"
)

func TestBuildSubmitTPDUsPreservesTextAndDestination(t *testing.T) {
	parts, err := BuildSubmitTPDUsWithOptions("+447700900123", " hello ", SubmitOptions{})
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
	parts, err := BuildSubmitTPDUsWithOptions("10086", "你好", SubmitOptions{Encoding: "ucs2"})
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
