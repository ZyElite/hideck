package config

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestInitGlobalManagerAddsDisabledHTTPSSettingToLegacyConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	path := writeTempConfig(t, `
server:
  # 保留原注释
  port: 7575
devices: []
`)

	if err := InitGlobalManager(path); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}
	if GetConfig().Server.HTTPSEnabled {
		t.Fatal("HTTPSEnabled = true, want false")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if !strings.Contains(content, "https_enabled: false") || !strings.Contains(content, "# 保留原注释") {
		t.Fatalf("migrated config =\n%s", content)
	}
}

func TestInitGlobalManagerPreservesEnabledHTTPSSetting(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	path := writeTempConfig(t, `
server:
  port: 7575
  https_enabled: true
devices: []
`)

	if err := InitGlobalManager(path); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}
	if !GetConfig().Server.HTTPSEnabled {
		t.Fatal("HTTPSEnabled = false, want true")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "https_enabled:") != 1 {
		t.Fatalf("existing setting was duplicated:\n%s", raw)
	}
}
