package api

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIDocumentsPersistentSMSReadState(t *testing.T) {
	data, err := os.ReadFile("openapi.vohive.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	paths := openAPIMap(t, document, "paths")
	thread := openAPIMap(t, paths, "/sms/thread")
	if thread["patch"] == nil {
		t.Fatal("OpenAPI /sms/thread missing PATCH operation")
	}
	get := openAPIMap(t, thread, "get")
	parameters, ok := get["parameters"].([]any)
	if !ok || !hasOpenAPIParameter(parameters, "iccid") {
		t.Fatal("OpenAPI GET /sms/thread missing ICCID parameter")
	}
	deleteOperation := openAPIMap(t, thread, "delete")
	deleteParameters, ok := deleteOperation["parameters"].([]any)
	if !ok || !hasOpenAPIParameter(deleteParameters, "iccid") {
		t.Fatal("OpenAPI DELETE /sms/thread missing ICCID parameter")
	}
	components := openAPIMap(t, document, "components")
	schemas := openAPIMap(t, components, "schemas")
	sendRequest := openAPIMap(t, schemas, "SMSSendRequest")
	sendProperties := openAPIMap(t, sendRequest, "properties")
	if sendProperties["iccid"] == nil {
		t.Fatal("OpenAPI SMSSendRequest missing ICCID selector")
	}
	contact := openAPIMap(t, schemas, "SMSContact")
	properties := openAPIMap(t, contact, "properties")
	if properties["iccid"] == nil || properties["unread_count"] == nil {
		t.Fatal("OpenAPI SMSContact missing server-side unread identity fields")
	}
}

func hasOpenAPIParameter(parameters []any, name string) bool {
	for _, item := range parameters {
		parameter, ok := item.(map[string]any)
		if ok && parameter["name"] == name {
			return true
		}
	}
	return false
}
