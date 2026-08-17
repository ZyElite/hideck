package device

import (
	"strings"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/pkg/logger"
)

// deviceIMEIBackfillNeeded 判断是否需要把运行时学到的 IMEI 回填进配置。
// 仅当运行时已学到非空 IMEI、且与配置记录不同(含配置侧为空)时才回填。
// 空 IMEI 绝不触发,确保永不擦除配置里已有的身份。
func deviceIMEIBackfillNeeded(stored, current config.DeviceConfig) bool {
	if strings.TrimSpace(current.ModemIMEI) == "" {
		return false
	}
	return config.NormalizeIMEI(stored.ModemIMEI) != config.NormalizeIMEI(current.ModemIMEI)
}

// persistDeviceAttachmentsIfChanged 设备启动/恢复完成后,只把运行时学到的 IMEI 回填进配置文件,
// 完成一次性身份锚定。绝不写回 control_device / interface / at_port / qmi_device / usb_path /
// audio_device 等易变路径——这些只活在内存,每次按 IMEI 现解析(见 spec 第 5 节)。
// 失败只记日志,不影响设备已成功启动这一事实。
func (p *Pool) persistDeviceAttachmentsIfChanged(cfg config.DeviceConfig) {
	if p == nil || strings.TrimSpace(cfg.ID) == "" {
		return
	}
	p.rememberRuntimeQMIAttachment(cfg)
	stored, err := config.GetDeviceByID(cfg.ID)
	if err != nil || stored == nil {
		return
	}
	if !deviceIMEIBackfillNeeded(*stored, cfg) {
		return
	}
	path := config.GetConfigPath()
	if strings.TrimSpace(path) == "" {
		return
	}
	imei := strings.TrimSpace(cfg.ModemIMEI)
	if err := config.UpdateDeviceIMEIInFile(path, map[string]string{cfg.ID: imei}); err != nil {
		logger.Warn("回填设备 IMEI 到配置文件失败", "device", cfg.ID, "err", err)
		return
	}
	if err := config.ReloadFromFile(); err != nil {
		logger.Warn("回填 IMEI 后重新加载配置文件失败", "device", cfg.ID, "err", err)
		return
	}
	logger.Info("已回填设备 IMEI 到配置文件", "device", cfg.ID, "imei", imei)
}

func (p *Pool) rememberRuntimeQMIAttachment(cfg config.DeviceConfig) {
	if p == nil {
		return
	}
	id := strings.TrimSpace(cfg.ID)
	if id == "" {
		return
	}
	snapshot := config.DeviceConfig{
		ControlDevice: strings.TrimSpace(cfg.ControlDevice),
		QMIDevice:     strings.TrimSpace(cfg.QMIDevice),
		Interface:     strings.TrimSpace(cfg.Interface),
		USBPath:       strings.TrimSpace(cfg.USBPath),
		ATPort:        strings.TrimSpace(cfg.ATPort),
		ModemIMEI:     strings.TrimSpace(cfg.ModemIMEI),
	}
	if snapshot.ControlDevice == "" && snapshot.Interface == "" && snapshot.USBPath == "" && snapshot.ModemIMEI == "" {
		return
	}
	p.mu.Lock()
	if p.runtimeQMIAttachments == nil {
		p.runtimeQMIAttachments = make(map[string]config.DeviceConfig)
	}
	p.runtimeQMIAttachments[id] = snapshot
	p.mu.Unlock()
}

func (p *Pool) overlayRuntimeQMIAttachment(cfg config.DeviceConfig) config.DeviceConfig {
	if p == nil {
		return cfg
	}
	id := strings.TrimSpace(cfg.ID)
	if id == "" {
		return cfg
	}
	p.mu.RLock()
	runtime, ok := p.runtimeQMIAttachments[id]
	p.mu.RUnlock()
	if !ok {
		return cfg
	}
	if strings.TrimSpace(cfg.ControlDevice) == "" {
		cfg.ControlDevice = runtime.ControlDevice
		if strings.TrimSpace(cfg.QMIDevice) == "" {
			cfg.QMIDevice = runtime.QMIDevice
			if cfg.QMIDevice == "" {
				cfg.QMIDevice = runtime.ControlDevice
			}
		}
	}
	if strings.TrimSpace(cfg.Interface) == "" {
		cfg.Interface = runtime.Interface
	}
	if strings.TrimSpace(cfg.USBPath) == "" {
		cfg.USBPath = runtime.USBPath
	}
	if strings.TrimSpace(cfg.ATPort) == "" {
		cfg.ATPort = runtime.ATPort
		if strings.TrimSpace(cfg.ManagePort) == "" {
			cfg.ManagePort = runtime.ATPort
		}
	}
	if strings.TrimSpace(cfg.ModemIMEI) == "" {
		cfg.ModemIMEI = runtime.ModemIMEI
	}
	return cfg
}

func (p *Pool) forgetRuntimeQMIAttachment(deviceID string) {
	if p == nil {
		return
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return
	}
	p.mu.Lock()
	delete(p.runtimeQMIAttachments, deviceID)
	p.mu.Unlock()
}
