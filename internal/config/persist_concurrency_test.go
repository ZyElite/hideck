package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

func TestConfigUpdatesSerializeAcrossSections(t *testing.T) {
	for iteration := range 12 {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("system:\n  openwrt_dynamic_interfaces: false\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		errors := make(chan error, 2)
		var wait sync.WaitGroup
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-start
			errors <- UpdateSystemInFile(path, SystemConfig{OpenWRTDynamicInterfaces: true})
		}()
		go func() {
			defer wait.Done()
			<-start
			errors <- UpdateNotificationInFile(path, NotificationConfigs{QQ: QQConfig{AppID: "qq-app"}})
		}()
		close(start)
		wait.Wait()
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatalf("iteration %d: %v", iteration, err)
			}
		}
		assertConcurrentConfigSections(t, path)
	}
}

func assertConcurrentConfigSections(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var loaded map[string]any
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	system, _ := loaded["system"].(map[string]any)
	qq, _ := loaded["qq"].(map[string]any)
	if system["openwrt_dynamic_interfaces"] != true || qq["app_id"] != "qq-app" {
		t.Fatalf("concurrent config = %+v", loaded)
	}
}
