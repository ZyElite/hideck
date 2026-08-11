package device

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBackendAttachmentDiscoveryWaitFindsTargetByIMEI(t *testing.T) {
	discovery := backendAttachmentTestDiscovery([]CompatibleModem{{
		ControlPath:   "/dev/cdc-wdm2",
		NetInterface:  "wwan2",
		USBPath:       "1-2",
		ATPort:        "/dev/ttyUSB6",
		IMEI:          "866069053342612",
		TransportType: "mbim",
	}})

	got, err := discovery.Wait(context.Background(), "866069053342612", "mbim")
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if got.Backend != "mbim" || got.ControlDevice != "/dev/cdc-wdm2" || got.ATPort != "/dev/ttyUSB6" {
		t.Fatalf("Wait() = %+v", got)
	}
}

func TestBackendAttachmentDiscoveryWaitRejectsAmbiguousIMEI(t *testing.T) {
	discovery := backendAttachmentTestDiscovery([]CompatibleModem{
		backendAttachmentTestModem("1-2", "/dev/cdc-wdm0", "wwan0"),
		backendAttachmentTestModem("1-3", "/dev/cdc-wdm1", "wwan1"),
	})
	discovery.PollInterval = time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, err := discovery.Wait(ctx, "866069053342612", "qmi")
	if err == nil || !strings.Contains(err.Error(), "拒绝自动选择") {
		t.Fatalf("Wait() error = %v", err)
	}
}

func TestBackendAttachmentDiscoveryWaitReportsTimeoutAndLastState(t *testing.T) {
	discovery := backendAttachmentTestDiscovery(nil)
	discovery.PollInterval = time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, err := discovery.Wait(ctx, "866069053342612", "qmi")
	if err == nil || !strings.Contains(err.Error(), "未发现 IMEI") || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("Wait() error = %v", err)
	}
}

func TestBackendAttachmentDiscoveryWaitRejectsUnknownTarget(t *testing.T) {
	discovery := backendAttachmentTestDiscovery(nil)
	_, err := discovery.Wait(context.Background(), "866069053342612", "auto")
	if err == nil || !strings.Contains(err.Error(), "仅支持 qmi 或 mbim") {
		t.Fatalf("Wait() error = %v", err)
	}
}

func backendAttachmentTestDiscovery(modems []CompatibleModem) BackendAttachmentDiscovery {
	return BackendAttachmentDiscovery{
		Scan: func() ([]CompatibleModem, error) {
			return modems, nil
		},
		Resolve: func(modem CompatibleModem, _ time.Duration) (CompatibleModem, string) {
			return modem, modem.IMEI
		},
		PollInterval: time.Millisecond,
		ProbeTimeout: time.Millisecond,
	}
}

func backendAttachmentTestModem(usbPath, controlPath, iface string) CompatibleModem {
	return CompatibleModem{
		ControlPath:   controlPath,
		NetInterface:  iface,
		USBPath:       usbPath,
		ATPort:        "/dev/ttyUSB2",
		IMEI:          "866069053342612",
		TransportType: "qmi",
	}
}
