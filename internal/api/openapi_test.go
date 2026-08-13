package api

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIVoHiveYAMLValid(t *testing.T) {
	data, err := os.ReadFile("openapi.vohive.yaml")
	if err != nil {
		t.Fatalf("read openapi.vohive.yaml: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("openapi.vohive.yaml is invalid YAML: %v", err)
	}
	if doc["openapi"] == "" {
		t.Fatalf("openapi.vohive.yaml missing openapi version")
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok || paths["/system/time"] == nil {
		t.Fatal("openapi.vohive.yaml missing /system/time")
	}
	for _, path := range []string{
		"/command-center/commands", "/command-center/executions", "/command-center/events",
		"/command-center/stream", "/command-center/history", "/balances",
		"/command-center/recordings/{recording}",
		"/devices/{device_id}/balance-queries", "/carrier-query-rules", "/carrier-query-rules/{rule_id}",
		"/devices/{device_id}/manual-balance",
		"/commands/catalog", "/commands/executions", "/commands/events",
		"/commands/events/stream", "/commands/history", "/balance/queries",
		"/balance/queries/{query_id}", "/balance/rules", "/balance/rules/{rule_id}",
		"/devices/{device_id}/esim/actions/disable",
	} {
		if paths[path] == nil {
			t.Fatalf("openapi.vohive.yaml missing %s", path)
		}
	}
	if !strings.Contains(string(data), "enum: [at, qmi, mbim, pcsc]") {
		t.Fatal("openapi.vohive.yaml missing MBIM/PCSC device and eSIM transport contract")
	}
}
