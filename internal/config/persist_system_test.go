package config

import (
	"os"
	"strings"
	"testing"
)

func TestUpdateSystemInFilePersistsOpenWRTSetting(t *testing.T) {
	path := writeTempConfig(t, `
server:
  port: 7575
devices:
  - id: wwan0
    modem_imei: "123456789012345"
`)
	if err := UpdateSystemInFile(path, SystemConfig{OpenWRTDynamicInterfaces: true}); err != nil {
		t.Fatalf("UpdateSystemInFile() error = %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.System.OpenWRTDynamicInterfaces {
		t.Fatal("OpenWRTDynamicInterfaces = false, want true")
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0].ModemIMEI != "123456789012345" {
		t.Fatalf("device config changed: %+v", cfg.Devices)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(raw), "openwrt_dynamic_interfaces: true") {
		t.Fatalf("persisted config =\n%s", raw)
	}
}

func TestSystemSettingDefaultsOff(t *testing.T) {
	path := writeTempConfig(t, "devices: []")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.System.OpenWRTDynamicInterfaces {
		t.Fatal("OpenWRTDynamicInterfaces = true, want default false")
	}
}
