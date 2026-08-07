package device

import (
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAddWorkerQMIManagedRebindsByIMEIWhenControlDeviceGone(t *testing.T) {
	originalDiscover := discoverQMIDevicesFn
	defer func() { discoverQMIDevicesFn = originalDiscover }()
	discoverQMIDevicesFn = func() ([]QMIDevice, error) {
		return []QMIDevice{
			{
				ControlPath:  "/dev/cdc-wdm-new-qmi",
				NetInterface: "wwan-new",
				USBPath:      "1-2.3",
				ATPort:       "/dev/ttyUSB-new",
			},
		}, nil
	}

	originalResolveQMI := resolveDiscoveredQMIDeviceFn
	defer func() { resolveDiscoveredQMIDeviceFn = originalResolveQMI }()
	resolveDiscoveredQMIDeviceFn = func(dev QMIDevice, timeout time.Duration, allowProbe bool) (QMIDevice, string) {
		if dev.ControlPath == "/dev/cdc-wdm-new-qmi" {
			return dev, "123456789012345"
		}
		return dev, ""
	}

	p := NewPool(&config.Config{})
	t.Cleanup(func() { require.NoError(t, p.Shutdown()) })

	devCfg := config.DeviceConfig{
		ID:             "dev-qmi-1",
		DeviceBackend:  "qmi",
		ModemIMEI:      "123456789012345",
		ControlDevice:  "/dev/nonexistent-control-old",
		Interface:      "wwan-old",
		USBPath:        "1-9.9",
		NetworkEnabled: true,
	}

	worker, err := p.AddWorkerFromConfig(devCfg)
	if err != nil {
		require.ErrorContains(t, err, "启动 QMI Core 失败")
		require.ErrorContains(t, err, "/dev/cdc-wdm-new-qmi")
		require.NotContains(t, err.Error(), "/dev/nonexistent-control-old")
		return
	}

	requireReboundQMIAttachment(t, worker)
}

func requireReboundQMIAttachment(t *testing.T, worker *Worker) {
	t.Helper()
	require.NotNil(t, worker)
	require.Equal(t, "/dev/cdc-wdm-new-qmi", worker.Config.ControlDevice)
	require.Equal(t, "/dev/cdc-wdm-new-qmi", worker.Config.QMIDevice)
	require.Equal(t, "wwan-new", worker.Config.Interface)
	require.Equal(t, "1-2.3", worker.Config.USBPath)
	require.Equal(t, "/dev/ttyUSB-new", worker.Config.ATPort)
	require.Equal(t, "/dev/ttyUSB-new", worker.Config.ManagePort)
}
