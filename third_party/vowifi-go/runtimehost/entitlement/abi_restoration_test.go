package entitlement

import (
	"reflect"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/sim"
)

func TestLegacyPublicStructPrefixes(t *testing.T) {
	type headerPairPrefix struct {
		Key   string
		Value string
	}
	type httpRequestPrefix struct {
		Method  string
		URL     string
		Headers []HeaderPair
		Body    []byte
	}
	type responsePrefix struct {
		StatusCode int
		Body       []byte
	}
	type identityPrefix struct {
		IMSI        string
		ICCID       string
		IMEI        string
		MCC         string
		MNC         string
		SIPUsername string
		DisplayName string
	}
	type tokenStatePrefix struct {
		Token    string
		AppToken string
	}
	type requestPrefix struct {
		Provider       string
		EntitlementURL string
		Identity       Identity
		Token          TokenState
		Client         HTTPClient
		AKAProvider    sim.AKAProvider
		Trace          TraceSink
	}

	assertLegacyStructPrefix(t, reflect.TypeOf(HTTPRequest{}), reflect.TypeOf(httpRequestPrefix{}))
	assertLegacyStructPrefix(t, reflect.TypeOf(HTTPResponse{}), reflect.TypeOf(responsePrefix{}))
	assertLegacyStructPrefix(t, reflect.TypeOf(HeaderPair{}), reflect.TypeOf(headerPairPrefix{}))
	assertLegacyStructPrefix(t, reflect.TypeOf(Identity{}), reflect.TypeOf(identityPrefix{}))
	assertLegacyStructPrefix(t, reflect.TypeOf(TokenState{}), reflect.TypeOf(tokenStatePrefix{}))
	assertLegacyStructPrefix(t, reflect.TypeOf(Request{}), reflect.TypeOf(requestPrefix{}))
}

func assertLegacyStructPrefix(t *testing.T, actual, prefix reflect.Type) {
	t.Helper()
	if actual.NumField() < prefix.NumField() {
		t.Fatalf("%s has %d fields, want at least %d", actual, actual.NumField(), prefix.NumField())
	}
	for index := 0; index < prefix.NumField(); index++ {
		got, want := actual.Field(index), prefix.Field(index)
		if got.Name != want.Name || got.Type != want.Type {
			t.Fatalf("%s field %d = %s %s, want %s %s", actual, index, got.Name, got.Type, want.Name, want.Type)
		}
	}
}
