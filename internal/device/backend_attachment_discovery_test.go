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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := make(chan error, 1)
	go func() {
		_, err := discovery.Wait(ctx, "866069053342612", "qmi")
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "拒绝自动选择") {
			t.Fatalf("Wait() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ambiguous attachment must fail without waiting for context cancellation")
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

func TestBackendAttachmentDiscoveryWaitCancelsBlockedResolver(t *testing.T) {
	discovery := backendAttachmentTestDiscovery([]CompatibleModem{
		backendAttachmentTestModem("1-2", "/dev/cdc-wdm1", "wwan1"),
	})
	discovery.Resolve = func(ctx context.Context, modem CompatibleModem, _ time.Duration) (CompatibleModem, string) {
		<-ctx.Done()
		return modem, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := discovery.Wait(ctx, "866069053342612", "qmi")
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("Wait() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("Wait() cancellation took %v", elapsed)
	}
}

func TestBackendAttachmentDiscoveryWaitCancelsBlockedIdentityProbe(t *testing.T) {
	discovery := backendAttachmentTestDiscovery([]CompatibleModem{
		backendAttachmentTestModem("1-2", "/dev/cdc-wdm1", "wwan1"),
	})
	discovery.ProbeIdentity = func(ctx context.Context, _ CompatibleModem, _ time.Duration) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := discovery.Wait(ctx, "866069053342612", "qmi")
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("Wait() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Wait() cancellation took %v", elapsed)
	}
}

func TestBackendAttachmentDiscoveryUsesOwnedATPortHintAfterProtocolIdentity(t *testing.T) {
	modem := backendAttachmentTestModem("1-2", "/dev/cdc-wdm1", "wwan1")
	modem.ATPort = "/dev/ttyUSB4"
	modem.ATPorts = []string{"/dev/ttyUSB4", "/dev/ttyUSB6"}
	discovery := backendAttachmentTestDiscovery([]CompatibleModem{modem})
	discovery.Resolve = func(context.Context, CompatibleModem, time.Duration) (CompatibleModem, string) {
		t.Fatal("AT resolver must not run when the owned port hint is present")
		return CompatibleModem{}, ""
	}

	got, err := discovery.WaitWithHint(
		context.Background(),
		BackendAttachmentQuery{
			IMEI:          "866069053342612",
			TargetBackend: "qmi",
			ATPortHint:    "/dev/ttyUSB6",
		},
	)
	if err != nil {
		t.Fatalf("WaitWithHint() error = %v", err)
	}
	if got.ATPort != "/dev/ttyUSB6" {
		t.Fatalf("ATPort = %q, want /dev/ttyUSB6", got.ATPort)
	}
}

func TestBackendAttachmentDiscoveryRebindsWhenATPortNumberChanges(t *testing.T) {
	modem := backendAttachmentTestModem("1-2", "/dev/cdc-wdm1", "wwan1")
	modem.ATPort = "/dev/ttyUSB8"
	modem.ATPorts = []string{"/dev/ttyUSB8", "/dev/ttyUSB9"}
	discovery := backendAttachmentTestDiscovery([]CompatibleModem{modem})
	discovery.Resolve = func(_ context.Context, candidate CompatibleModem, _ time.Duration) (CompatibleModem, string) {
		candidate.ATPort = "/dev/ttyUSB8"
		return candidate, "866069053342612"
	}

	got, err := discovery.WaitWithHint(
		context.Background(),
		BackendAttachmentQuery{
			IMEI:          "866069053342612",
			TargetBackend: "qmi",
			ATPortHint:    "/dev/ttyUSB6",
		},
	)
	if err != nil {
		t.Fatalf("WaitWithHint() error = %v", err)
	}
	if got.ATPort != "/dev/ttyUSB8" {
		t.Fatalf("ATPort = %q, want /dev/ttyUSB8", got.ATPort)
	}
}

func TestBackendAttachmentDiscoveryUsesExplicitATIdentityRecovery(t *testing.T) {
	modem := backendAttachmentTestModem("1-2", "/dev/cdc-wdm1", "wwan1")
	modem.TransportType = "mbim"
	modem.ATPorts = []string{"/dev/ttyUSB6"}
	discovery := backendAttachmentTestDiscovery([]CompatibleModem{modem})
	discovery.ProbeIdentity = func(context.Context, CompatibleModem, time.Duration) (string, error) {
		return "", context.DeadlineExceeded
	}
	discovery.Resolve = func(_ context.Context, candidate CompatibleModem, _ time.Duration) (CompatibleModem, string) {
		candidate.ATPort = "/dev/ttyUSB6"
		return candidate, "866069053342612"
	}

	got, err := discovery.WaitWithHint(context.Background(), BackendAttachmentQuery{
		IMEI:                    "866069053342612",
		TargetBackend:           "mbim",
		ATPortHint:              "/dev/ttyUSB6",
		AllowATIdentityRecovery: true,
	})
	if err != nil {
		t.Fatalf("WaitWithHint() error = %v", err)
	}
	if got.IdentitySource != "at_recovery" || !strings.Contains(got.IdentityWarning, "deadline exceeded") {
		t.Fatalf("identity evidence = source %q warning %q", got.IdentitySource, got.IdentityWarning)
	}
}

func TestBackendIdentityProbeTimeoutDependsOnProtocol(t *testing.T) {
	discovery := BackendAttachmentDiscovery{}
	if got := discovery.identityProbeTimeout("qmi"); got != defaultBackendIdentityProbeTimeout {
		t.Fatalf("QMI identity timeout = %s, want %s", got, defaultBackendIdentityProbeTimeout)
	}
	if got := discovery.identityProbeTimeout("mbim"); got != defaultMBIMIdentityProbeTimeout {
		t.Fatalf("MBIM identity timeout = %s, want %s", got, defaultMBIMIdentityProbeTimeout)
	}

	discovery.MBIMIdentityTimeout = 17 * time.Second
	if got := discovery.identityProbeTimeout("MBIM"); got != 17*time.Second {
		t.Fatalf("custom MBIM identity timeout = %s, want 17s", got)
	}
}

func backendAttachmentTestDiscovery(modems []CompatibleModem) BackendAttachmentDiscovery {
	return BackendAttachmentDiscovery{
		Scan: func() ([]CompatibleModem, error) {
			return modems, nil
		},
		ProbeIdentity: func(_ context.Context, modem CompatibleModem, _ time.Duration) (string, error) {
			return modem.IMEI, nil
		},
		Resolve: func(_ context.Context, modem CompatibleModem, _ time.Duration) (CompatibleModem, string) {
			return modem, modem.IMEI
		},
		PollInterval:         time.Millisecond,
		ProbeTimeout:         time.Millisecond,
		IdentityProbeTimeout: time.Millisecond,
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
