package api

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIDocumentsPhoneSurfaceAndLease(t *testing.T) {
	data, err := os.ReadFile("openapi.hideck.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	paths := openAPIMap(t, document, "paths")
	for _, path := range phoneOpenAPIPaths() {
		if paths[path] == nil {
			t.Errorf("OpenAPI missing phone path %s", path)
		}
	}
	components := openAPIMap(t, document, "components")
	parameters := openAPIMap(t, components, "parameters")
	assertPhoneLeaseParameter(t, parameters, "PhoneLease", true)
	assertPhoneLeaseParameter(t, parameters, "PhoneLeaseOptional", false)
	schemas := openAPIMap(t, components, "schemas")
	for _, schema := range []string{"PhoneCall", "PhoneCallRecord", "PhoneEvent", "PhoneMediaAnswer"} {
		if schemas[schema] == nil {
			t.Errorf("OpenAPI missing phone schema %s", schema)
		}
	}
}

func phoneOpenAPIPaths() []string {
	return []string{
		"/phone/ca.crt", "/phone/devices", "/phone/media", "/phone/calls",
		"/phone/calls/active", "/phone/calls/{call_id}/answer", "/phone/calls/{call_id}/reject",
		"/phone/calls/{call_id}/dtmf", "/phone/calls/{call_id}", "/phone/calls/{call_id}/media",
		"/phone/events", "/phone/history", "/phone/recordings/{recording}",
	}
}

func assertPhoneLeaseParameter(t *testing.T, parameters map[string]any, name string, required bool) {
	t.Helper()
	parameter := openAPIMap(t, parameters, name)
	if parameter["in"] != "header" || parameter["name"] != "X-Phone-Lease" || parameter["required"] != required {
		t.Errorf("%s = %+v", name, parameter)
	}
}

func openAPIMap(t *testing.T, source map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := source[key].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI %s is unavailable", key)
	}
	return value
}
