package profile

import (
	"strings"
	"testing"
)

func TestNormalizeAKAApp(t *testing.T) {
	if got := NormalizeAKAApp("isim_strict"); got != AKAAppPreferenceISIMStrict {
		t.Errorf("isim_strict = %q", got)
	}
	if got := NormalizeAKAApp("USIM"); got != AKAAppPreferenceUSIM {
		t.Errorf("USIM = %q", got)
	}
	if got := NormalizeAKAApp(""); got != AKAAppPreferenceISIM {
		t.Errorf("default = %q", got)
	}
}

func TestAuthPlanNormalize(t *testing.T) {
	p := &AuthPlan{ISIMAvailable: true}
	p.Normalize()
	if p.AKAApp != AKAAppPreferenceISIM {
		t.Errorf("aka app = %q", p.AKAApp)
	}
	if p.IsZero() {
		t.Error("plan with ISIM should not be zero")
	}
}

func TestGenerateStableIMEIForModel(t *testing.T) {
	imei := GenerateStableIMEIForModel("iPhone 15", "310260123456789")
	if len(imei) != 15 {
		t.Fatalf("imei len = %d", len(imei))
	}
	// Luhn check.
	sum := 0
	for i := 0; i < 15; i++ {
		d := int(imei[i] - '0')
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	if sum%10 != 0 {
		t.Errorf("imei %s fails Luhn", imei)
	}
	// Deterministic.
	if GenerateStableIMEIForModel("iPhone 15", "310260123456789") != imei {
		t.Error("imei should be deterministic")
	}
}

func TestResolveUserAgentForModel(t *testing.T) {
	if got := ResolveUserAgentForModel("iPhone 14"); got != "iPhone" {
		t.Errorf("ua = %q", got)
	}
	if got := ResolveUserAgentForModel("unknown"); got != "vowifi" {
		t.Errorf("ua default = %q", got)
	}
}

func TestNormalizeProfile(t *testing.T) {
	p := Normalize(Profile{IMSI: "310260123456789"})
	if p.MCC != "310" || p.MNC != "26" {
		t.Errorf("normalized = %+v", p)
	}
}

func TestParseTransparentFileSizeFromFCP(t *testing.T) {
	// FCP: 62 04 80 02 00 10 (container length 4).
	fcp := []byte{0x62, 0x04, 0x80, 0x02, 0x00, 0x10}
	size, err := parseTransparentFileSizeFromFCP(fcp)
	if err != nil || size != 16 {
		t.Errorf("size = %d err %v", size, err)
	}
}

func TestCollectTLVValues(t *testing.T) {
	tlvs := collectTLVValues([]byte{0x80, 0x02, 0x00, 0x10, 0x88, 0x01, 0x05})
	if len(tlvs) != 2 {
		t.Fatalf("tlvs = %d", len(tlvs))
	}
	if tlvs[0x88][0] != 5 {
		t.Errorf("0x88 = %v", tlvs[0x88])
	}
}

func TestNormalizeIdentityString(t *testing.T) {
	if got := normalizeIdentityString([]byte("  user@example.com\x00 ")); got != "user@example.com" {
		t.Errorf("normalized = %q", got)
	}
}

func TestAppendUnique(t *testing.T) {
	got := appendUnique([]string{"a"}, "a")
	if len(got) != 1 {
		t.Error("duplicate should not be appended")
	}
	got = appendUnique([]string{"a"}, "b")
	if len(got) != 2 {
		t.Error("new value should be appended")
	}
}

func TestImeiLuhnCheckDigit(t *testing.T) {
	// For prefix "35693803564380", the check digit is 9.
	if got := imeiLuhnCheckDigit("35693803564380"); got != '9' {
		t.Errorf("check digit = %c", got)
	}
}

var _ = strings.Contains
