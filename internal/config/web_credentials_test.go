package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTracksWebPasswordSource(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("web:\n  username: admin\n  password: file-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("config file", func(t *testing.T) {
		t.Setenv(WebPasswordEnvironmentVariable, "")
		cfg, err := Load(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Web.Password != "file-password" || cfg.Web.PasswordSource != WebPasswordSourceConfigFile {
			t.Fatalf("web credentials=%+v", cfg.Web)
		}
	})

	t.Run("environment override", func(t *testing.T) {
		t.Setenv(WebPasswordEnvironmentVariable, "environment-password")
		cfg, err := Load(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Web.Password != "environment-password" || cfg.Web.PasswordSource != WebPasswordSourceEnvironment {
			t.Fatalf("web credentials=%+v", cfg.Web)
		}
	})
}
