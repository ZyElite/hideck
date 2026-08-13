package api

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIDocumentsAutomaticTaskSurface(t *testing.T) {
	data, err := os.ReadFile("openapi.vohive.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI: %v", err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("parse OpenAPI: %v", err)
	}
	paths, ok := document["paths"].(map[string]any)
	if !ok {
		t.Fatal("OpenAPI paths are unavailable")
	}
	for _, path := range []string{
		"/automatic-tasks", "/automatic-tasks/{task_id}",
		"/automatic-tasks/{task_id}/actions/run", "/automatic-task-runs",
	} {
		if paths[path] == nil {
			t.Fatalf("OpenAPI missing %s", path)
		}
	}
}
