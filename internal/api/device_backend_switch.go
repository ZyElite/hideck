package api

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/device"
)

const backendSwitchDiscoveryTimeout = 12 * time.Second

type deviceBackendSwitcher interface {
	Switch(context.Context, backendSwitchRequest) (backendSwitchResult, error)
}

type backendSwitchPool interface {
	GetWorker(string) *device.Worker
	RemoveWorker(string) error
	AddWorkerFromConfig(config.DeviceConfig) (*device.Worker, error)
}

type backendAttachmentWaiter interface {
	Wait(context.Context, string, string) (device.BackendAttachment, error)
}

type backendSwitchRequest struct {
	Current config.DeviceConfig
	Desired config.DeviceConfig
	Target  string
}

type backendSwitchResult struct {
	DeviceID          string                   `json:"device_id"`
	TargetBackend     string                   `json:"target_backend"`
	ConfiguredBackend string                   `json:"configured_backend"`
	ActualBackend     string                   `json:"actual_backend,omitempty"`
	HardwareChanged   bool                     `json:"hardware_changed"`
	HardwareVerified  bool                     `json:"hardware_verified"`
	Persisted         bool                     `json:"persisted"`
	WorkerStarted     bool                     `json:"worker_started"`
	Attachment        device.BackendAttachment `json:"attachment,omitempty"`
}

type backendSwitchFailure struct {
	Stage  string              `json:"stage"`
	Result backendSwitchResult `json:"result"`
	Err    error               `json:"-"`
}

func (e *backendSwitchFailure) Error() string {
	return fmt.Sprintf("设备后端切换在 %s 阶段失败: %v", e.Stage, e.Err)
}

func (e *backendSwitchFailure) Unwrap() error { return e.Err }

type backendSwitchService struct {
	mu         sync.Mutex
	pool       backendSwitchPool
	discovery  backendAttachmentWaiter
	workerIMEI func(context.Context, *device.Worker) (string, error)
	openAT     func(string) (manualATSession, error)
	persist    func(string, string, config.DeviceConfig) error
	configPath string
}

func newDeviceBackendSwitchService(pool *device.Pool, configPath string) *backendSwitchService {
	discovery := device.NewBackendAttachmentDiscovery()
	return &backendSwitchService{
		pool:       pool,
		discovery:  discovery,
		workerIMEI: readBackendSwitchWorkerIMEI,
		openAT:     openManualATSession,
		persist:    config.UpdateDeviceInFile,
		configPath: configPath,
	}
}

func (s *backendSwitchService) Switch(
	ctx context.Context,
	req backendSwitchRequest,
) (backendSwitchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := validateBackendSwitchRequest(req)
	if err != nil {
		return result, backendSwitchError("validate", result, err)
	}
	if s.pool == nil || s.discovery == nil || s.workerIMEI == nil || s.openAT == nil || s.persist == nil {
		return result, backendSwitchError("initialize", result, fmt.Errorf("后端切换服务未完整初始化"))
	}

	currentAttachment, worker, err := s.resolveCurrentAttachment(ctx, req.Current)
	if err != nil {
		return result, backendSwitchError("discover_current", result, err)
	}
	result.ActualBackend = currentAttachment.Backend
	if worker != nil {
		if err := s.pool.RemoveWorker(req.Current.ID); err != nil {
			return result, backendSwitchError("stop_worker", result, err)
		}
	}

	actualMode, changed, err := s.applyHardwareMode(currentAttachment.ATPort, result.TargetBackend)
	result.ActualBackend = backendForUSBNetMode(actualMode)
	result.HardwareChanged = changed
	if err != nil {
		return result, backendSwitchError("apply_hardware", result, err)
	}
	return s.verifyPersistAndStart(ctx, req, result)
}

func (s *backendSwitchService) verifyPersistAndStart(
	ctx context.Context,
	req backendSwitchRequest,
	result backendSwitchResult,
) (backendSwitchResult, error) {
	verified, err := s.waitAttachment(ctx, req.Current.ModemIMEI, result.TargetBackend)
	if err != nil {
		return result, backendSwitchError("verify_reenumeration", result, err)
	}
	if verified.Backend != result.TargetBackend || !config.IMEIMatches(req.Current.ModemIMEI, verified.IMEI) {
		return result, backendSwitchError(
			"verify_reenumeration",
			result,
			fmt.Errorf("重枚举结果与目标设备不一致: backend=%q imei=%q", verified.Backend, verified.IMEI),
		)
	}
	result.ActualBackend = verified.Backend
	result.HardwareVerified = true
	result.Attachment = verified

	desired := applyVerifiedBackendAttachment(req.Desired, verified, result.TargetBackend)
	if err := s.persist(s.configPath, desired.ID, desired); err != nil {
		return result, backendSwitchError("persist_config", result, err)
	}
	result.Persisted = true
	if _, err := s.pool.AddWorkerFromConfig(desired); err != nil {
		return result, backendSwitchError("start_worker", result, err)
	}
	result.WorkerStarted = true
	return result, nil
}

func (s *backendSwitchService) resolveCurrentAttachment(
	ctx context.Context,
	cfg config.DeviceConfig,
) (device.BackendAttachment, *device.Worker, error) {
	worker := s.pool.GetWorker(cfg.ID)
	if worker == nil {
		attachment, err := s.waitAttachment(ctx, cfg.ModemIMEI, "")
		return attachment, nil, err
	}

	imeiCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	liveIMEI, err := s.workerIMEI(imeiCtx, worker)
	if err != nil {
		return device.BackendAttachment{}, worker, fmt.Errorf("读取在线设备 IMEI 失败: %w", err)
	}
	if !config.IMEIMatches(cfg.ModemIMEI, liveIMEI) {
		return device.BackendAttachment{}, worker, fmt.Errorf(
			"在线设备 IMEI %q 与配置 IMEI %q 不一致",
			liveIMEI,
			cfg.ModemIMEI,
		)
	}
	attachment := device.BackendAttachment{
		Backend:       strings.ToLower(strings.TrimSpace(cfg.DeviceBackend)),
		IMEI:          strings.TrimSpace(liveIMEI),
		ControlDevice: strings.TrimSpace(worker.Config.ControlDevice),
		Interface:     strings.TrimSpace(worker.Config.Interface),
		USBPath:       strings.TrimSpace(worker.Config.USBPath),
		ATPort:        worker.ResolvedATPort(),
	}
	if attachment.ATPort == "" {
		return device.BackendAttachment{}, worker, fmt.Errorf("在线设备没有可用 AT 端口")
	}
	return attachment, worker, nil
}

func readBackendSwitchWorkerIMEI(ctx context.Context, worker *device.Worker) (string, error) {
	if worker == nil || worker.Backend == nil {
		return "", fmt.Errorf("设备后端未初始化")
	}
	return worker.Backend.GetIMEI(ctx)
}

func validateBackendSwitchRequest(req backendSwitchRequest) (backendSwitchResult, error) {
	target := strings.ToLower(strings.TrimSpace(req.Target))
	configured := strings.ToLower(strings.TrimSpace(req.Current.DeviceBackend))
	result := backendSwitchResult{
		DeviceID:          strings.TrimSpace(req.Current.ID),
		TargetBackend:     target,
		ConfiguredBackend: configured,
	}
	if result.DeviceID == "" || result.DeviceID != strings.TrimSpace(req.Desired.ID) {
		return result, fmt.Errorf("设备 ID 无效或不一致")
	}
	if configured != "qmi" && configured != "mbim" {
		return result, fmt.Errorf("当前配置后端 %q 不支持自动切换，仅支持 qmi 或 mbim", configured)
	}
	if target != "qmi" && target != "mbim" {
		return result, fmt.Errorf("目标后端 %q 无效，仅支持 qmi 或 mbim", target)
	}
	if config.NormalizeIMEI(req.Current.ModemIMEI) == "" {
		return result, fmt.Errorf("设备未绑定有效 IMEI，拒绝切换")
	}
	if !config.IMEIMatches(req.Current.ModemIMEI, req.Desired.ModemIMEI) {
		return result, fmt.Errorf("切换期间不允许修改设备 IMEI")
	}
	return result, nil
}

func (s *backendSwitchService) waitAttachment(
	ctx context.Context,
	imei string,
	target string,
) (device.BackendAttachment, error) {
	discoveryCtx, cancel := context.WithTimeout(ctx, backendSwitchDiscoveryTimeout)
	defer cancel()
	return s.discovery.Wait(discoveryCtx, imei, target)
}

func applyVerifiedBackendAttachment(
	cfg config.DeviceConfig,
	attachment device.BackendAttachment,
	target string,
) config.DeviceConfig {
	mode := usbNetModeForBackend(target)
	cfg.DeviceBackend = target
	cfg.USBNetMode = &mode
	cfg.ModemIMEI = attachment.IMEI
	cfg.ControlDevice = attachment.ControlDevice
	cfg.QMIDevice = attachment.ControlDevice
	cfg.Interface = attachment.Interface
	cfg.USBPath = attachment.USBPath
	cfg.ATPort = attachment.ATPort
	cfg.ManagePort = attachment.ATPort
	return cfg
}

func usbNetModeForBackend(backend string) int {
	if backend == "mbim" {
		return 2
	}
	return 0
}

func backendForUSBNetMode(mode int) string {
	switch mode {
	case 0:
		return "qmi"
	case 2:
		return "mbim"
	default:
		return fmt.Sprintf("unknown(%d)", mode)
	}
}

func backendSwitchError(stage string, result backendSwitchResult, err error) error {
	return &backendSwitchFailure{Stage: stage, Result: result, Err: err}
}
