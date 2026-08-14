package config

import (
	"bytes"
	"os"
	"testing"

	"github.com/spf13/viper"
)

func TestInitGlobalManagerKeepsLegacyHTTPSWithoutRewritingConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	path := writeTempConfig(t, `
server:
  # 保留原注释
  port: 7575
devices: []
`)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := InitGlobalManager(path); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}
	if !GetConfig().Server.HTTPSEnabled {
		t.Fatal("HTTPSEnabled = false, want legacy-compatible true")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("InitGlobalManager rewrote config:\n%s", after)
	}
}

func TestInitGlobalManagerRespectsExplicitlyDisabledHTTPS(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	path := writeTempConfig(t, `
server:
  port: 7575
  https_enabled: false
devices: []
`)

	if err := InitGlobalManager(path); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}
	if GetConfig().Server.HTTPSEnabled {
		t.Fatal("HTTPSEnabled = true, want explicit false")
	}
}
