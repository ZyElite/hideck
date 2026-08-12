package config

import (
	"os"
	"path/filepath"
	"testing"
)

// 期望终态:Update/Add 保存设备时不把运行时路径写进 config(只存 IMEI + 意图)。
// 当前实现会写 control_device/interface/at_port → 本测试现在应 FAIL,证明保存侧泄漏。
func TestUpdateDeviceInFileDoesNotPersistRuntimePaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := "devices:\n- id: dev1\n  device_backend: qmi\n  modem_imei: \"867383058993207\"\n  vowifi_enabled: true\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// 模拟切运营商/编辑:传入带运行时解析路径的 cfg。
	usbNetMode := 2
	newDev := DeviceConfig{
		ID:            "dev1",
		ModemIMEI:     "867383058993207",
		DeviceBackend: "qmi",
		USBNetMode:    &usbNetMode,
		VoWiFiEnabled: true,
		ControlDevice: "/dev/cdc-wdm3", // 运行时路径,不应被持久化
		Interface:     "wwan2",
		ATPort:        "/dev/ttyUSB9",
		USBPath:       "/sys/bus/usb/devices/1-7",
	}
	if err := UpdateDeviceInFile(path, "dev1", newDev); err != nil {
		t.Fatalf("UpdateDeviceInFile() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	d := got.Devices[0]
	if d.ModemIMEI != "867383058993207" || d.DeviceBackend != "qmi" {
		t.Fatalf("identity/intent fields lost: %+v", d)
	}
	if d.USBNetMode == nil || *d.USBNetMode != 2 {
		t.Fatalf("verified usbnet mode lost: %+v", d)
	}
	if d.ControlDevice != "" || d.Interface != "" || d.ATPort != "" || d.USBPath != "" {
		t.Fatalf("runtime paths must not be persisted, got: %+v", d)
	}
}

// Add 新设备时同样不写路径(只 IMEI + 意图)。
func TestAddDeviceInFileDoesNotPersistRuntimePaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("devices: []\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	usbNetMode := 2
	dev := DeviceConfig{
		ID: "dev9", ModemIMEI: "861234567890123", DeviceBackend: "mbim",
		USBNetMode:    &usbNetMode,
		ControlDevice: "/dev/cdc-wdm5", Interface: "wwan5", ATPort: "/dev/ttyUSB1",
		USBPath: "/sys/bus/usb/devices/2-1",
	}
	if err := AddDeviceInFile(path, dev); err != nil {
		t.Fatalf("AddDeviceInFile() error = %v", err)
	}
	got, _ := Load(path)
	d := got.Devices[0]
	if d.ModemIMEI != "861234567890123" || d.DeviceBackend != "mbim" {
		t.Fatalf("identity/intent lost: %+v", d)
	}
	if d.USBNetMode == nil || *d.USBNetMode != 2 {
		t.Fatalf("verified usbnet mode lost: %+v", d)
	}
	if d.ControlDevice != "" || d.Interface != "" || d.ATPort != "" || d.USBPath != "" {
		t.Fatalf("runtime paths must not be persisted, got: %+v", d)
	}
}

func TestAddDeviceInFilePersistsPCSCBindingAndPINReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("devices: []\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	device := DeviceConfig{
		ID: "reader1", DeviceBackend: ESIMTransportPCSC, ESIMTransport: ESIMTransportPCSC,
		PCSCReaderName: "Example Reader 00 00", PCSCUSBPath: "/sys/bus/usb/devices/1-2",
		SIMPINEnv: "VOHIVE_SIM_PIN_READER1",
	}
	if err := AddDeviceInFile(path, device); err != nil {
		t.Fatalf("AddDeviceInFile() error = %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := loaded.Devices[0]
	if got.DeviceBackend != ESIMTransportPCSC || got.PCSCReaderName != device.PCSCReaderName ||
		got.PCSCUSBPath != device.PCSCUSBPath || got.SIMPINEnv != device.SIMPINEnv {
		t.Fatalf("PC/SC binding fields were not preserved: %+v", got)
	}
}

func TestUpdateDeviceInFilePersistsPCSCBindingAndPINReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := "devices:\n- id: reader1\n  device_backend: pcsc\n  pcsc_reader_name: Old Reader\n  pcsc_usb_path: old-path\n  sim_pin_env: OLD_PIN\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	updated := DeviceConfig{
		ID: "reader1", DeviceBackend: ESIMTransportPCSC,
		PCSCReaderName: "New Reader 00 00", PCSCUSBPath: "/sys/bus/usb/devices/2-3",
		SIMPINEnv: "VOHIVE_SIM_PIN_READER1",
	}
	if err := UpdateDeviceInFile(path, "reader1", updated); err != nil {
		t.Fatalf("UpdateDeviceInFile() error = %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := loaded.Devices[0]
	if got.PCSCReaderName != updated.PCSCReaderName || got.PCSCUSBPath != updated.PCSCUSBPath || got.SIMPINEnv != updated.SIMPINEnv {
		t.Fatalf("updated PC/SC fields were not preserved: %+v", got)
	}
}
