package device

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/pkg/logger"
)

var (
	errMBIMRegistrationDenied = errors.New("mbim_registration_denied")
	errMBIMSIMNotReady        = errors.New("mbim_sim_not_ready")
)

const (
	mbimRegistrationTimeoutDataRequired = 90 * time.Second
	mbimRegistrationTimeoutBestEffort   = 20 * time.Second
	mbimRegistrationRadioCycleAfter     = 30
)

type mbimRegistrationController interface {
	GetServingSystem(ctx context.Context) (*backend.ServingSystem, error)
	GetOperatingMode(ctx context.Context) (backend.OperatingMode, error)
	SetOperatingMode(ctx context.Context, mode backend.OperatingMode) error
	SetOperatorSelection(ctx context.Context, req backend.SetOperatorSelectionRequest) (backend.OperatorSelection, error)
	AttachPacketService(ctx context.Context) error
	IsSimInserted(ctx context.Context) (bool, error)
}

type mbimRegistrationOptions struct {
	PollInterval            time.Duration
	MaxAttempts             int
	RadioCycleAfterAttempts int
	SuppressRadioCycle      func() bool
	ShouldAbort             func() bool
}

type mbimRegistrationCommand struct {
	deviceID    string
	config      config.DeviceConfig
	controller  mbimRegistrationController
	shouldAbort func() bool
}

func normalizeMBIMRegistrationOptions(opts mbimRegistrationOptions) mbimRegistrationOptions {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 45
	}
	if opts.RadioCycleAfterAttempts <= 0 {
		opts.RadioCycleAfterAttempts = mbimRegistrationRadioCycleAfter
	}
	return opts
}

func mbimRegistrationTimeout(requiredForData bool) time.Duration {
	if requiredForData {
		return mbimRegistrationTimeoutDataRequired
	}
	return mbimRegistrationTimeoutBestEffort
}

func ensureMBIMRegistration(ctx context.Context, deviceID string, cfg config.DeviceConfig, ctrl mbimRegistrationController, opts mbimRegistrationOptions) error {
	if ctrl == nil {
		return fmt.Errorf("mbim registration controller unavailable")
	}
	opts = normalizeMBIMRegistrationOptions(opts)
	command := mbimRegistrationCommand{
		deviceID:    deviceID,
		config:      cfg,
		controller:  ctrl,
		shouldAbort: opts.ShouldAbort,
	}
	if err := ensureCellularRegistrationAllowed(opts.ShouldAbort); err != nil {
		return err
	}

	mode, err := ctrl.GetOperatingMode(ctx)
	if err != nil {
		return fmt.Errorf("读取 MBIM radio mode 失败: %w", err)
	}
	if isFlightOperatingMode(mode) {
		if err := ensureCellularRegistrationAllowed(opts.ShouldAbort); err != nil {
			return err
		}
		logger.Info("MBIM radio 初始处于飞行/低功耗，恢复 Online 后再驻网", "device", deviceID, "mode", int(mode))
		if err := ctrl.SetOperatingMode(ctx, backend.ModeOnline); err != nil {
			return fmt.Errorf("MBIM radio mode 恢复 Online 失败: %w", err)
		}
		if err := sleepQMIRegistrationPoll(ctx, opts.PollInterval); err != nil {
			return err
		}
	}

	if err := waitMBIMSIMReady(ctx, deviceID, ctrl, opts); err != nil {
		return err
	}

	registerIssued := false
	attachIssued := false
	radioCycleIssued := false
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		if err := ensureCellularRegistrationAllowed(opts.ShouldAbort); err != nil {
			return err
		}
		ss, err := ctrl.GetServingSystem(ctx)
		if err != nil {
			return fmt.Errorf("读取 MBIM serving system 失败: %w", err)
		}
		if ss == nil {
			return fmt.Errorf("读取 MBIM serving system 返回空结果")
		}

		switch ss.RegStatus {
		case 1, 5:
			if ss.PSAttached {
				return nil
			}
			if !attachIssued {
				if err := ensureCellularRegistrationAllowed(opts.ShouldAbort); err != nil {
					return err
				}
				logger.Info("MBIM 已驻网但 packet service 未 attach，发起 attach", "device", deviceID, "reg_status", ss.RegStatus)
				if err := ctrl.AttachPacketService(ctx); err != nil {
					return fmt.Errorf("MBIM packet service attach 失败: %w", err)
				}
				attachIssued = true
			}
		case 2:
			if !registerIssued {
				if err := command.initiate(ctx); err != nil {
					return err
				}
				registerIssued = true
			}
			if registerIssued && !radioCycleIssued && attempt >= opts.RadioCycleAfterAttempts {
				if opts.SuppressRadioCycle != nil && opts.SuppressRadioCycle() {
					logger.Info("MBIM 驻网恢复暂缓 radio cycle：运营商扫描进行中", "device", deviceID, "attempt", attempt)
				} else {
					radioCycleIssued = true
					if err := command.radioCycle(ctx, opts.PollInterval); err != nil {
						if errors.Is(err, errQMIRegistrationSkipped) {
							return err
						}
						logger.Warn("MBIM 驻网恢复 radio cycle 失败，继续等待模组自主驻网", "device", deviceID, "err", err)
					}
					registerIssued = false
					attachIssued = false
				}
			}
		case 3:
			return fmt.Errorf("%w: %s", errMBIMRegistrationDenied, ss.RegStatusText)
		default:
			if !registerIssued {
				if err := command.initiate(ctx); err != nil {
					return err
				}
				registerIssued = true
			}
		}

		if err := sleepQMIRegistrationPoll(ctx, opts.PollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("MBIM 驻网/packet attach 超时: attempts=%d", opts.MaxAttempts)
}

func waitMBIMSIMReady(ctx context.Context, deviceID string, ctrl mbimRegistrationController, opts mbimRegistrationOptions) error {
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		if err := ensureCellularRegistrationAllowed(opts.ShouldAbort); err != nil {
			return err
		}
		inserted, err := ctrl.IsSimInserted(ctx)
		if err != nil {
			return fmt.Errorf("读取 MBIM SIM 状态失败: %w", err)
		}
		if inserted {
			return nil
		}
		logger.Debug("MBIM SIM 尚未 READY，等待后重试", "device", deviceID, "attempt", attempt)
		if err := sleepQMIRegistrationPoll(ctx, opts.PollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("%w: attempts=%d", errMBIMSIMNotReady, opts.MaxAttempts)
}

func (c mbimRegistrationCommand) initiate(ctx context.Context) error {
	if err := ensureCellularRegistrationAllowed(c.shouldAbort); err != nil {
		return err
	}
	req := backend.SetOperatorSelectionRequest{Mode: backend.OperatorSelectionAutomatic}
	if c.config.OperatorSelectionMode == string(backend.OperatorSelectionManual) && c.config.OperatorSelectionPLMN != "" {
		req = backend.SetOperatorSelectionRequest{
			Mode: backend.OperatorSelectionManual,
			PLMN: c.config.OperatorSelectionPLMN,
			RAT:  backend.OperatorAccessTechnology(c.config.OperatorSelectionRAT),
		}
	}
	if _, err := c.controller.SetOperatorSelection(ctx, req); err != nil {
		return fmt.Errorf("MBIM 注册选择提交失败: %w", err)
	}
	logger.Info("MBIM 注册选择已提交", "device", c.deviceID, "mode", req.Mode, "plmn", req.PLMN)
	return nil
}

func (c mbimRegistrationCommand) radioCycle(ctx context.Context, wait time.Duration) error {
	if wait <= 0 {
		wait = 2 * time.Second
	}
	logger.Info("MBIM 搜网持续未恢复，执行 radio flight-mode cycle 重新触发搜网", "device", c.deviceID)
	if err := ensureCellularRegistrationAllowed(c.shouldAbort); err != nil {
		return err
	}
	if err := c.controller.SetOperatingMode(ctx, backend.ModeRFOff); err != nil {
		return fmt.Errorf("设置 RFOff 失败: %w", err)
	}
	if err := sleepQMIRegistrationPoll(ctx, wait); err != nil {
		return err
	}
	if err := ensureCellularRegistrationAllowed(c.shouldAbort); err != nil {
		return err
	}
	if err := c.controller.SetOperatingMode(ctx, backend.ModeOnline); err != nil {
		return fmt.Errorf("恢复 Online 失败: %w", err)
	}
	if err := sleepQMIRegistrationPoll(ctx, wait); err != nil {
		return err
	}
	return nil
}

func (w *Worker) EnsureMBIMRegistration(ctx context.Context, requiredForData bool) error {
	err := w.ensureMBIMRegistration(ctx, requiredForData)
	if err == nil {
		return nil
	}
	if errors.Is(err, errQMIRegistrationSkipped) {
		if requiredForData {
			return fmt.Errorf("蜂窝射频被 VoWiFi/飞行策略抑制: %w", err)
		}
		return nil
	}
	if requiredForData {
		return err
	}
	logger.Warn("MBIM 驻网协调失败，数据网络未开启，按非致命处理", "device", w.ID, "err", err)
	return nil
}

func (w *Worker) ensureMBIMRegistration(ctx context.Context, requiredForData bool) error {
	if w == nil || w.MBIMCore == nil || w.Backend == nil {
		return nil
	}
	if w.shouldAbortCellularRegistration() {
		logger.Debug("MBIM 驻网协调跳过：蜂窝射频被 VoWiFi/飞行策略抑制", "device", w.ID)
		return errQMIRegistrationSkipped
	}
	ctrl, ok := w.Backend.(mbimRegistrationController)
	if !ok {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, mbimRegistrationTimeout(requiredForData))
	defer cancel()
	return ensureMBIMRegistration(ctx, w.ID, w.Config, ctrl, mbimRegistrationOptions{
		SuppressRadioCycle: w.IsOperatorScanActive,
		ShouldAbort:        w.shouldAbortCellularRegistration,
	})
}

func (w *Worker) StartMBIMRegistrationReconcile(ctx context.Context, reason string) bool {
	if w == nil || w.MBIMCore == nil || w.Backend == nil {
		return false
	}
	if w.shouldAbortCellularRegistration() {
		logger.Debug("MBIM 后台驻网协调跳过：蜂窝射频被 VoWiFi/飞行策略抑制", "device", w.ID, "reason", reason)
		return false
	}
	return w.startQMIRegistrationReconcile(ctx, reason, func(runCtx context.Context) error {
		return w.EnsureMBIMRegistration(runCtx, false)
	})
}
