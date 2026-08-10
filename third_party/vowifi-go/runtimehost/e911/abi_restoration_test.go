package e911

import (
	"reflect"
	"testing"
)

func TestLegacyHTTPStructPrefixes(t *testing.T) {
	type headerPairPrefix struct {
		Key   string
		Value string
	}
	type requestPrefix struct {
		Method  string
		URL     string
		Headers []HeaderPair
		Body    []byte
	}
	type responsePrefix struct {
		StatusCode int
		Body       []byte
	}

	assertLegacyStructPrefix(t, reflect.TypeOf(HTTPRequest{}), reflect.TypeOf(requestPrefix{}))
	assertLegacyStructPrefix(t, reflect.TypeOf(HTTPResponse{}), reflect.TypeOf(responsePrefix{}))
	assertLegacyStructPrefix(t, reflect.TypeOf(HeaderPair{}), reflect.TypeOf(headerPairPrefix{}))
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
