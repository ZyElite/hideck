package api

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIHiDeckYAMLValid(t *testing.T) {
	data, err := os.ReadFile("openapi.hideck.yaml")
	if err != nil {
		t.Fatalf("read openapi.hideck.yaml: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("openapi.hideck.yaml is invalid YAML: %v", err)
	}
	if doc["openapi"] == "" {
		t.Fatalf("openapi.hideck.yaml missing openapi version")
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok || paths["/system/time"] == nil {
		t.Fatal("openapi.hideck.yaml missing /system/time")
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
		"/settings/notifications/wecom/test",
	} {
		if paths[path] == nil {
			t.Fatalf("openapi.hideck.yaml missing %s", path)
		}
	}
	if !strings.Contains(string(data), "enum: [at, qmi, mbim, pcsc]") {
		t.Fatal("openapi.hideck.yaml missing MBIM/PCSC device and eSIM transport contract")
	}
}
