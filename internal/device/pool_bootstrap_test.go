package device

import (
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAddWorkerQMIManagedRebindsByIMEIWhenControlDeviceGone(t *testing.T) {
	// QMI 托管设备：配置 control_device 指向不存在的旧节点，但保留正确 IMEI；
	// 注入一块带该 IMEI 的新路径 QMI 硬件，bootstrap 应按 IMEI 找回并采纳新路径。
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

	// 初始化 Pool，并在用例结束时停止可能进入后台重试的 QMI Core。
	p := NewPool(&config.Config{})
	t.Cleanup(func() { require.NoError(t, p.Shutdown()) })

	devCfg := config.DeviceConfig{
		ID:             "dev-qmi-1",
		DeviceBackend:  "qmi",
		ModemIMEI:      "123456789012345",
		ControlDevice:  "/dev/nonexistent-control-old",
		Interface:      "wwan-old",
		USBPath:        "1-9.9",
		NetworkEnabled: true, // 满足 hasManagedQMINetwork，允许按 IMEI 重新发现。
	}

	// 旧 control_device 不存在时，bootstrap 会先按 IMEI 采用新路径，再启动 QMI Core。
	// 不同传输路径的后续结果不同：可能立即返回新路径打开失败，也可能转入后台重试并返回 Worker；
	// 该启动结果不影响本用例的核心目标——确认重绑使用的是新路径，而不是继续使用旧路径。
	worker, err := p.AddWorkerFromConfig(devCfg)
	if err != nil {
		// 立即失败时，错误必须来自新控制路径，这本身即可证明重绑已经完成。
		require.ErrorContains(t, err, "启动 QMI Core 失败")
		require.ErrorContains(t, err, "/dev/cdc-wdm-new-qmi")
		require.NotContains(t, err.Error(), "/dev/nonexistent-control-old")
		return
	}

	// 后台重试路径会返回 Worker，直接检查完整的运行时挂载信息。
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
