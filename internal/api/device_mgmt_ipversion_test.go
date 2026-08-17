package api

import (
	"testing"

	"github.com/yibaiba/hideck/internal/config"
)

func TestValidateManagedNetworkConfigRejectsBadIPVersion(t *testing.T) {
	err := validateManagedNetworkConfig(config.DeviceConfig{
		ID:        "dev-ipv",
		IPVersion: "v9",
	})
	if err == nil {
		t.Fatal("validateManagedNetworkConfig() error = nil, want invalid ip_version")
	}
}

func TestDeviceConfigForAddDropsDevicePolicyFields(t *testing.T) {
	cfg := deviceConfigForAdd(config.DeviceConfig{
		ID:              "dev-1",
		APN:             "ims",
		IPVersion:       "bad",
		NetworkEnabled:  true,
		VoWiFiEnabled:   true,
		AirplaneEnabled: true,
		SMSEnabled:      false,
	})

	if cfg.APN != "" || cfg.IPVersion != "" || cfg.NetworkEnabled || cfg.VoWiFiEnabled {
		t.Fatalf("设备添加不应保留卡上的网络/电话策略字段: %+v", cfg)
	}
	if !cfg.AirplaneEnabled {
		t.Fatal("第一次添加设备应默认飞行模式")
	}
	if !cfg.SMSEnabled {
		t.Fatal("SMS 应保持系统不变量 true")
	}
}
