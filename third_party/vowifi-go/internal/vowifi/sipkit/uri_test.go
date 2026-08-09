package sipkit

import "testing"

func TestParseURIValidation(t *testing.T) {
	valid := []string{"sip:user@example.com", "user@example.com", "tel:+441234", "sip:"}
	for _, value := range valid {
		if err := ParseURI(value); err != nil {
			t.Errorf("ParseURI(%q): %v", value, err)
		}
	}
	for _, value := range []string{"", "tel:   ", "urn:service:sos"} {
		if err := ParseURI(value); err == nil {
			t.Errorf("ParseURI(%q) succeeded", value)
		}
	}
}

func TestParseAORWithDefaultHost(t *testing.T) {
	for _, value := range []string{"user", "sip:user", "sips:user;user=phone", "sip:user@"} {
		if err := ParseAORWithDefaultHost(value, "IMS.Example"); err != nil {
			t.Errorf("ParseAORWithDefaultHost(%q): %v", value, err)
		}
	}
	if err := ParseAORWithDefaultHost("user", ""); err == nil {
		t.Fatal("missing default host accepted")
	}
}

func TestExtractURIFromHeaderValue(t *testing.T) {
	tests := map[string]string{
		`Display <sip:user@example.com>;tag=abc`: "sip:user@example.com",
		`user@example.com;tag=abc`:               "sip:user@example.com",
		`tel:+441234`:                            "tel:+441234",
	}
	for input, want := range tests {
		got, err := ExtractURIFromHeaderValue(input)
		if err != nil || got != want {
			t.Errorf("ExtractURIFromHeaderValue(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}

func TestParseHostPortWithDefault(t *testing.T) {
	tests := []struct {
		input string
		host  string
		port  int
	}{
		{"example.com:5070", "example.com", 5070},
		{"example.com", "example.com", 5060},
		{"[2001:DB8::1]:5080", "[2001:DB8::1]", 5080},
		{"sip:user@example.com:5090;transport=tcp", "example.com", 5090},
		{"example.com:bad", "example.com", 5060},
	}
	for _, test := range tests {
		host, port, err := ParseHostPortWithDefault(test.input, 5060)
		if err != nil || host != test.host || port != test.port {
			t.Errorf("ParseHostPortWithDefault(%q) = %q, %d, %v", test.input, host, port, err)
		}
	}
}

func TestNormalizeHostPreservesCaseAndBracketsIPv6(t *testing.T) {
	if got := NormalizeHost(" EXAMPLE.com "); got != "EXAMPLE.com" {
		t.Fatalf("NormalizeHost DNS = %q", got)
	}
	if got := NormalizeHost("[2001:DB8::1]"); got != "[2001:DB8::1]" {
		t.Fatalf("NormalizeHost IPv6 = %q", got)
	}
	if !hasURIScheme("URN:service:sos") || hasURIScheme("user@example.com") {
		t.Fatal("URI scheme detection mismatch")
	}
}
