package profile

import (
	"strings"
	"testing"
)

func TestKnownModelTableAndUserAgents(t *testing.T) {
	for _, model := range knownDeviceModels {
		canonical, tac, ok := resolveKnownModelTAC(model.canonical)
		if !ok || canonical != model.canonical || tac != model.tac {
			t.Fatalf("resolveKnownModelTAC(%q) = %q, %q, %t", model.canonical, canonical, tac, ok)
		}
		if got := ResolveUserAgentForModel(model.canonical); got == defaultUserAgent && model.canonical != "iphone15,4" {
			if userAgents[model.canonical] != defaultUserAgent {
				t.Fatalf("missing user agent for %q", model.canonical)
			}
		}
	}
	canonical, tac, ok := resolveKnownModelTAC("device SM-S928B")
	if !ok || canonical != "galaxy_s24_ultra" || tac != "35819412" {
		t.Fatalf("alias resolution = %q, %q, %t", canonical, tac, ok)
	}
	if got := ResolveUserAgentForModel("unknown"); got != defaultUserAgent {
		t.Fatalf("default UA = %q", got)
	}
}

func TestGenerateStableIMEIForModel(t *testing.T) {
	got := GenerateStableIMEIForModel("234102356143376", "rmx3366")
	if len(got) != 15 || !strings.HasPrefix(got, "86034905") {
		t.Fatalf("GenerateStableIMEIForModel() = %q", got)
	}
	if got != GenerateStableIMEIForModel("234102356143376", "rmx3366") {
		t.Fatal("stable IMEI changed for the same seed")
	}
	if GenerateStableIMEIForModel("seed", "unknown") != "" {
		t.Fatal("unknown model generated an IMEI")
	}
	if check := imeiLuhnCheckDigit(got[:14]); check != got[14:] {
		t.Fatalf("Luhn digit = %q, want %q", check, got[14:])
	}
	if imeiLuhnCheckDigit("123") != "" || imeiLuhnCheckDigit("1234567890123x") != "" {
		t.Fatal("invalid IMEI prefix accepted")
	}
}
