package device

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/damonto/euicc-go/driver"
	"github.com/yibaiba/hideck/internal/apduarbiter"
	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/esim"
	"github.com/yibaiba/hideck/internal/pcsc"
	"github.com/yibaiba/hideck/pkg/logger"
	"github.com/yibaiba/hideck/pkg/smscodec"
)

func pcscSelector(cfg config.DeviceConfig) pcsc.Selector {
	return pcsc.Selector{
		USBPath: strings.TrimSpace(cfg.PCSCUSBPath), ReaderName: strings.TrimSpace(cfg.PCSCReaderName),
	}
}

func (p *Pool) addPCSCWorkerFromConfig(devCfg config.DeviceConfig, attempt uint64) (*Worker, error) {
	service := p.sharedPCSCService()
	reader, err := resolvePCSCReader(p.ctx, service, pcscSelector(devCfg))
	if err != nil {
		return nil, err
	}
	devCfg = bindPCSCReader(devCfg, reader)
	deviceBackend, err := newPCSCDeviceBackend(service, pcscSelector(devCfg), devCfg.SIMPINEnv)
	if err != nil {
		return nil, err
	}
	w := newPCSCWorker(p, devCfg, service, deviceBackend)
	p.assignWorkerGeneration(w)
	w.EsimMgr = p.newPCSCEsimManager(w, service, pcscSelector(devCfg))
	if !p.isRebuildAttemptCurrent(devCfg.ID, attempt) {
		_ = deviceBackend.Close()
		return nil, fmt.Errorf("设备 %s 启动流程已超时放弃", devCfg.ID)
	}
	p.mu.Lock()
	p.workers[devCfg.ID] = w
	p.mu.Unlock()
	if err := w.RefreshIdentityLive(nil, "pcsc_startup"); err != nil {
		p.removeWorkerRegistrationIfCurrent(w)
		_ = deviceBackend.Close()
		return nil, err
	}
	w.setCachedHealthy(true)
	p.PersistIdentityState(w)
	if w.CurrentICCID() != "" {
		p.resolveAndApplyPolicy(w, "pcsc_startup")
	}
	logger.Info("PC/SC 读卡器设备已启动", "device", devCfg.ID, "reader", reader.Name, "usb_path", reader.USBPath)
	return w, nil
}

func (p *Pool) sharedPCSCService() *pcsc.Service {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pcscService == nil {
		p.pcscService = pcsc.New()
	}
	return p.pcscService
}

func (p *Pool) reconcilePCSCReaders(opts rescanReconnectOptions, allDevices, devices []config.DeviceConfig) error {
	if len(devices) == 0 {
		return nil
	}
	p.pcscReconcileMu.Lock()
	defer p.pcscReconcileMu.Unlock()
	readers, err := p.sharedPCSCService().Readers(p.ctx)
	if err != nil {
		return fmt.Errorf("PC/SC 读卡器重扫失败: %w", err)
	}
	for _, cfg := range devices {
		if !FreeDeviceLimitAllowsConfiguredDevice(allDevices, cfg.ID) {
			continue
		}
		reader, found := pcsc.MatchReader(readers, pcscSelector(cfg))
		worker := p.GetWorker(cfg.ID)
		if !found || !reader.CardPresent {
			p.removeMissingPCSCWorker(opts, cfg, worker)
			continue
		}
		if worker != nil || !opts.allowWorkerMutation(cfg.ID) {
			continue
		}
		logger.Info("检测到 PC/SC 卡片上线，自动启动", "device", cfg.ID, "reader", reader.Name)
		if _, err := p.AddWorkerFromConfig(bindPCSCReader(cfg, reader)); err != nil {
			logger.Warn("自动启动 PC/SC 设备失败", "device", cfg.ID, "err", err)
			continue
		}
		if cfg.VoWiFiEnabled {
			go p.enablePCSCVoWiFiAfterRecovery(cfg.ID)
		}
	}
	return nil
}

func (p *Pool) pcscMonitorLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			devices := config.ListDevices()
			_, readers := partitionManagedDevices(devices)
			if err := p.reconcilePCSCReaders(rescanReconnectOptions{}, devices, readers); err != nil {
				logger.Debug("PC/SC 卡片状态轮询失败", "err", err)
			}
		}
	}
}

func (p *Pool) removeMissingPCSCWorker(opts rescanReconnectOptions, cfg config.DeviceConfig, worker *Worker) {
	if worker == nil || !opts.allowWorkerMutation(cfg.ID) {
		return
	}
	logger.Info("检测到 PC/SC 卡片离线，清理 Worker", "device", cfg.ID, "reader", cfg.PCSCReaderName)
	p.teardownVoWiFiForReconnect(cfg.ID)
	if p.lifecycle != nil {
		p.lifecycle.BeginRecovery(cfg.ID, LifecyclePhaseUSBWait, "pcsc_card_missing", qmiLifecycleRecoveryTTL)
	}
	if err := p.RemoveWorker(cfg.ID); err != nil {
		logger.Warn("清理 PC/SC Worker 失败", "device", cfg.ID, "err", err)
	}
}

func (p *Pool) enablePCSCVoWiFiAfterRecovery(deviceID string) {
	if err := p.enableVoWiFiWhenReady(deviceID, 5*time.Second, "pcsc_card_recovery"); err != nil {
		logger.Warn("PC/SC 卡片恢复后自动重启 VoWiFi 失败", "device", deviceID, "err", err)
	}
}

func resolvePCSCReader(ctx context.Context, service *pcsc.Service, selector pcsc.Selector) (pcsc.Reader, error) {
	readers, err := service.Readers(ctx)
	if err != nil {
		return pcsc.Reader{}, fmt.Errorf("发现 PC/SC 读卡器失败: %w", err)
	}
	reader, ok := pcsc.MatchReader(readers, selector)
	if !ok {
		return pcsc.Reader{}, pcsc.ErrReaderNotFound
	}
	if !reader.CardPresent {
		return pcsc.Reader{}, pcsc.ErrNoCard
	}
	return reader, nil
}

func bindPCSCReader(cfg config.DeviceConfig, reader pcsc.Reader) config.DeviceConfig {
	cfg.PCSCReaderName = reader.Name
	cfg.PCSCUSBPath = reader.USBPath
	cfg.ControlDevice = reader.Name
	cfg.USBPath = reader.USBPath
	cfg.DeviceBackend = config.ESIMTransportPCSC
	cfg.ESIMTransport = config.ESIMTransportPCSC
	return cfg
}

func newPCSCWorker(p *Pool, cfg config.DeviceConfig, service *pcsc.Service, deviceBackend *pcscDeviceBackend) *Worker {
	return &Worker{
		ID: cfg.ID, Config: cfg, Backend: deviceBackend, PCSCService: service,
		APDUArbiter: apduarbiter.New(cfg.ID, apduarbiter.Options{MaxLeaseHold: 10 * time.Minute, MaxSessions: 3}),
		Pool:        p, stop: make(chan struct{}), reassembler: smscodec.NewReassembler(), smsMode: smsModeVoWiFi,
	}
}

func (p *Pool) newPCSCEsimManager(worker *Worker, service *pcsc.Service, selector pcsc.Selector) *esim.Manager {
	channelFactory := func() (driver.SmartCardChannel, error) {
		return pcsc.NewChannel(service, selector, true), nil
	}
	onBefore, onAfter, onFailed, onDegraded, onPhase := p.newESIMSwitchCallbacks(worker.ID)
	return esim.NewManagerWithSmartCardChannelFactoryCallbacks(worker.ID, channelFactory, nil, esim.ChannelFactorySwitchCallbacks{
		OnBeforeSwitch: onBefore,
		OnAfterSwitch: func(_ esim.SwitchOperation, token uint64) {
			onAfter(token)
		},
		OnSwitchFailed: func(_ esim.SwitchOperation, token uint64, err error) {
			onFailed(token, err)
		},
		OnSwitchDegraded: func(_ esim.SwitchOperation, token uint64, phase esim.SwitchPhase, err error) {
			onDegraded(token, phase, err)
		},
		OnSwitchPhase: func(_ esim.SwitchOperation, token uint64, phase esim.SwitchPhase) {
			onPhase(token, phase)
		},
	})
}
