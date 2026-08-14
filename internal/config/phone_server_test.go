package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestPhoneServerDefaultsAndAddressNormalization(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 7575\nweb:\n  username: admin\n  password: secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Server.HTTPSEnabled || cfg.Server.Port != ":7575" || cfg.Server.HTTPSPort != ":7576" ||
		cfg.Server.WebRTCUDPAddress != ":7580" || cfg.Server.WebRTCPublicHost != "" ||
		cfg.Server.TLSDataDir != "data/tls" {
		t.Fatalf("server defaults = %+v", cfg.Server)
	}
}

func TestWebRTCPublicHostEnvironmentOverride(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("PROXY_SERVER_WEBRTC_PUBLIC_HOST", "hideck.example.com")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 7575\nweb:\n  username: admin\n  password: secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.WebRTCPublicHost != "hideck.example.com" {
		t.Fatalf("server.webrtc_public_host = %q", cfg.Server.WebRTCPublicHost)
	}
}
