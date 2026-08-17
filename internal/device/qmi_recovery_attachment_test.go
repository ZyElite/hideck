package device

import (
	"os"
	"testing"
	"time"

	"github.com/yibaiba/hideck/internal/config"
)

func TestQMIRecoveryAttachmentResolverPrefersIMEIMatch(t *testing.T) {
	pool := NewPool(&config.Config{})
	defer pool.cancel()

	restore := stubQMIRecoveryDiscovery(t, []QMIDevice{
		{ControlPath: "/dev/cdc-wdm0", NetInterface: "wwan0", USBPath: "1-1"},
		{ControlPath: "/dev/cdc-wdm2", NetInterface: "wwan2", USBPath: "1-2"},
	}, map[string]string{
		"/dev/cdc-wdm0": "864388041069422",
		"/dev/cdc-wdm2": "867383058993207",
	})
	defer restore()

	decision := pool.ResolveQMIRecoveryAttachment(config.DeviceConfig{
		ID:            "dev-qmi",
		DeviceBackend: "qmi",
		ModemIMEI:     "867383058993207",
		ControlDevice: "/dev/cdc-wdm0",
		Interface:     "wwan0",
	})

	if !decision.Ready {
		t.Fatalf("Ready=false, reason=%s", decision.Reason)
	}
	if decision.Attachment.ControlPath != "/dev/cdc-wdm2" {
		t.Fatalf("ControlPath=%q want /dev/cdc-wdm2", decision.Attachment.ControlPath)
	}
}

func TestQMIRecoveryAttachmentResolverFallsBackToStableStaticPath(t *testing.T) {
	pool := NewPool(&config.Config{})
	defer pool.cancel()

	restore := stubQMIRecoveryDiscovery(t, []QMIDevice{
		{ControlPath: "/dev/cdc-wdm0", NetInterface: "wwan0", USBPath: "1-1"},
	}, map[string]string{})
	defer restore()

	decision := pool.ResolveQMIRecoveryAttachment(config.DeviceConfig{
		ID:            "dev-qmi",
		DeviceBackend: "qmi",
		ControlDevice: "/dev/cdc-wdm0",
		Interface:     "wwan0",
	})

	if !decision.Ready {
		t.Fatalf("Ready=false, reason=%s", decision.Reason)
	}
	if decision.Attachment.ControlPath != "/dev/cdc-wdm0" {
		t.Fatalf("ControlPath=%q want /dev/cdc-wdm0", decision.Attachment.ControlPath)
	}
}

func TestQMIRecoveryAttachmentResolverRejectsMismatchedIMEI(t *testing.T) {
	pool := NewPool(&config.Config{})
	defer pool.cancel()

	restore := stubQMIRecoveryDiscovery(t, []QMIDevice{
		{ControlPath: "/dev/cdc-wdm0", NetInterface: "wwan0", USBPath: "1-1"},
	}, map[string]string{
		"/dev/cdc-wdm0": "864388041069422",
	})
	defer restore()

	decision := pool.ResolveQMIRecoveryAttachment(config.DeviceConfig{
		ID:            "dev-qmi",
		DeviceBackend: "qmi",
		ModemIMEI:     "867383058993207",
		ControlDevice: "/dev/cdc-wdm2", // Intentional mismatch
		Interface:     "wwan2",
	})

	if decision.Ready {
		t.Fatalf("Ready=true, want false with mismatched IMEI and mismatched path: %#v", decision)
	}
}

func stubQMIRecoveryDiscovery(t *testing.T, devices []QMIDevice, imeis map[string]string) func() {
	t.Helper()
	origDiscover := discoverQMIDevicesFn
	origResolve := resolveDiscoveredQMIDeviceFn
	origStat := qmiControlStatFn

	discoverQMIDevicesFn = func() ([]QMIDevice, error) {
		return append([]QMIDevice(nil), devices...), nil
	}
	resolveDiscoveredQMIDeviceFn = func(dev QMIDevice, timeout time.Duration, allowIMEIProbe bool) (QMIDevice, string) {
		return dev, imeis[dev.ControlPath]
	}
	qmiControlStatFn = func(name string) (os.FileInfo, error) {
		return nil, nil
	}

	return func() {
		discoverQMIDevicesFn = origDiscover
		resolveDiscoveredQMIDeviceFn = origResolve
		qmiControlStatFn = origStat
	}
}

func TestQMIRecoveryAttachmentResolverIMEINotMatchFallbacksToPathMatch(t *testing.T) {
	pool := NewPool(&config.Config{})
	defer pool.cancel()

	restore := stubQMIRecoveryDiscovery(t, []QMIDevice{
		{ControlPath: "/dev/cdc-wdm0", NetInterface: "wwan0", USBPath: "1-1"},
	}, map[string]string{
		"/dev/cdc-wdm0": "864388041069422", // simulated IMEI mismatch
	})
	defer restore()

	decision := pool.ResolveQMIRecoveryAttachment(config.DeviceConfig{
		ID:            "dev-qmi",
		DeviceBackend: "qmi",
		ModemIMEI:     "867383058993207",
		ControlDevice: "/dev/cdc-wdm0",
		Interface:     "wwan0",
	})

	if !decision.Ready {
		t.Fatalf("Ready=false, reason=%s", decision.Reason)
	}
	if decision.Attachment.ControlPath != "/dev/cdc-wdm0" {
		t.Fatalf("ControlPath=%q want /dev/cdc-wdm0", decision.Attachment.ControlPath)
	}
}

func TestQMIRecoveryAttachmentResolverIMEINotMatchNoPathMatchNotReady(t *testing.T) {
	pool := NewPool(&config.Config{})
	defer pool.cancel()

	restore := stubQMIRecoveryDiscovery(t, []QMIDevice{
		{ControlPath: "/dev/cdc-wdm2", NetInterface: "wwan2", USBPath: "1-2"},
	}, map[string]string{
		"/dev/cdc-wdm2": "864388041069422",
	})
	defer restore()

	decision := pool.ResolveQMIRecoveryAttachment(config.DeviceConfig{
		ID:            "dev-qmi",
		DeviceBackend: "qmi",
		ModemIMEI:     "867383058993207",
		ControlDevice: "/dev/cdc-wdm0",
		Interface:     "wwan0",
	})

	if decision.Ready {
		t.Fatalf("Ready=true, want false when IMEI and path both don't match")
	}
}
