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
		"/settings/notifications/weixin/qr/start", "/settings/notifications/weixin/qr/status",
		"/settings/notifications/weixin/qr/cancel", "/settings/notifications/wecom-bot/qr/start",
		"/settings/notifications/wecom-bot/qr/status", "/settings/notifications/wecom-bot/qr/cancel",
		"/settings/notifications/qq/qr/start", "/settings/notifications/qq/qr/status",
		"/settings/notifications/qq/qr/cancel", "/settings/notifications/feishu/qr/start",
		"/settings/notifications/feishu/qr/status", "/settings/notifications/feishu/qr/cancel",
		"/settings/disclaimer",
	} {
		if paths[path] == nil {
			t.Fatalf("openapi.hideck.yaml missing %s", path)
		}
	}
	if !strings.Contains(string(data), "enum: [at, qmi, mbim, pcsc]") {
		t.Fatal("openapi.hideck.yaml missing MBIM/PCSC device and eSIM transport contract")
	}
	for _, field := range []string{
		"wecom_bot:", "allowed_group_ids:", "manual_setup_available:",
		"bound_chat_id:", "BotFather 签发的 Bot Token", "授权管理员首次私聊自动绑定",
	} {
		if !strings.Contains(string(data), field) {
			t.Fatalf("openapi.hideck.yaml missing notification field %s", field)
		}
	}
}

func TestOpenAPIHiDeckLocalReferencesResolve(t *testing.T) {
	data, err := os.ReadFile("openapi.hideck.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if ref, ok := typed["$ref"].(string); ok && strings.HasPrefix(ref, "#/") {
				assertOpenAPIReference(t, doc, ref)
			}
			for _, child := range typed {
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		}
	}
	visit(doc)
}

func assertOpenAPIReference(t *testing.T, doc map[string]any, ref string) {
	t.Helper()
	var current any = doc
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		mapping, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("OpenAPI reference %s traverses a non-object at %s", ref, part)
		}
		current, ok = mapping[part]
		if !ok {
			t.Fatalf("OpenAPI reference %s does not resolve", ref)
		}
	}
}
