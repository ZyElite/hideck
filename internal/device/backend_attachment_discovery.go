package device

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/config"
)

const defaultBackendAttachmentPollInterval = 500 * time.Millisecond

// BackendAttachment is the verified runtime attachment for one managed modem.
type BackendAttachment struct {
	Backend       string `json:"backend"`
	IMEI          string `json:"imei"`
	ControlDevice string `json:"control_device"`
	Interface     string `json:"interface"`
	USBPath       string `json:"usb_path"`
	ATPort        string `json:"at_port"`
}

type BackendAttachmentDiscovery struct {
	Scan         func() ([]CompatibleModem, error)
	Resolve      func(CompatibleModem, time.Duration) (CompatibleModem, string)
	PollInterval time.Duration
	ProbeTimeout time.Duration
}

func NewBackendAttachmentDiscovery() BackendAttachmentDiscovery {
	return BackendAttachmentDiscovery{
		Scan:         DiscoverCompatibleModems,
		Resolve:      ResolveCompatibleModemATPort,
		PollInterval: defaultBackendAttachmentPollInterval,
		ProbeTimeout: 1200 * time.Millisecond,
	}
}

func (d BackendAttachmentDiscovery) Wait(
	ctx context.Context,
	imei string,
	targetBackend string,
) (BackendAttachment, error) {
	if config.NormalizeIMEI(imei) == "" {
		return BackendAttachment{}, fmt.Errorf("IMEI 无效，无法验证重枚举设备")
	}
	target, err := normalizeAttachmentBackend(targetBackend)
	if err != nil {
		return BackendAttachment{}, err
	}
	if d.Scan == nil || d.Resolve == nil {
		return BackendAttachment{}, fmt.Errorf("设备发现器未初始化")
	}

	pollInterval := d.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultBackendAttachmentPollInterval
	}
	var lastErr error
	for {
		attachment, findErr := d.findOnce(imei, target)
		if findErr == nil {
			return attachment, nil
		}
		lastErr = findErr

		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return BackendAttachment{}, fmt.Errorf("等待设备重枚举失败: %w（最后状态: %v）", ctx.Err(), lastErr)
		case <-timer.C:
		}
	}
}

func normalizeAttachmentBackend(raw string) (string, error) {
	backend := strings.ToLower(strings.TrimSpace(raw))
	switch backend {
	case "", "qmi", "mbim":
		return backend, nil
	default:
		return "", fmt.Errorf("不支持按 %q 验证设备，仅支持 qmi 或 mbim", backend)
	}
}

func (d BackendAttachmentDiscovery) findOnce(imei, target string) (BackendAttachment, error) {
	modems, err := d.Scan()
	if err != nil {
		return BackendAttachment{}, fmt.Errorf("扫描设备失败: %w", err)
	}

	matches := make(map[string]BackendAttachment)
	for _, raw := range modems {
		mode := strings.ToLower(strings.TrimSpace(raw.TransportType))
		if mode == "" {
			mode = strings.ToLower(strings.TrimSpace(raw.Mode))
		}
		if mode != "qmi" && mode != "mbim" {
			continue
		}
		if target != "" && mode != target {
			continue
		}

		resolved, probedIMEI := d.Resolve(raw, d.probeTimeout())
		resolvedIMEI := strings.TrimSpace(probedIMEI)
		if resolvedIMEI == "" {
			resolvedIMEI = strings.TrimSpace(resolved.IMEI)
		}
		if !config.IMEIMatches(imei, resolvedIMEI) {
			continue
		}
		attachment, attachmentErr := attachmentFromCompatibleModem(resolved, resolvedIMEI, mode)
		if attachmentErr != nil {
			return BackendAttachment{}, attachmentErr
		}
		matches[attachmentKey(attachment)] = attachment
	}

	if len(matches) == 0 {
		if target == "" {
			return BackendAttachment{}, fmt.Errorf("未发现 IMEI %s 对应的 QMI/MBIM 设备", imei)
		}
		return BackendAttachment{}, fmt.Errorf("未发现 IMEI %s 对应的 %s 设备", imei, strings.ToUpper(target))
	}
	if len(matches) > 1 {
		return BackendAttachment{}, fmt.Errorf("IMEI %s 对应到 %d 个设备路径，拒绝自动选择", imei, len(matches))
	}
	for _, attachment := range matches {
		return attachment, nil
	}
	return BackendAttachment{}, fmt.Errorf("设备发现结果为空")
}

func (d BackendAttachmentDiscovery) probeTimeout() time.Duration {
	if d.ProbeTimeout > 0 {
		return d.ProbeTimeout
	}
	return 1200 * time.Millisecond
}

func attachmentFromCompatibleModem(
	modem CompatibleModem,
	imei string,
	backend string,
) (BackendAttachment, error) {
	attachment := BackendAttachment{
		Backend:       backend,
		IMEI:          imei,
		ControlDevice: strings.TrimSpace(modem.ControlPath),
		Interface:     strings.TrimSpace(modem.NetInterface),
		USBPath:       strings.TrimSpace(modem.USBPath),
		ATPort:        strings.TrimSpace(modem.ATPort),
	}
	if attachment.ControlDevice == "" || attachment.Interface == "" || attachment.ATPort == "" {
		return BackendAttachment{}, fmt.Errorf(
			"IMEI %s 的 %s 枚举不完整: control=%q interface=%q at=%q",
			imei,
			strings.ToUpper(backend),
			attachment.ControlDevice,
			attachment.Interface,
			attachment.ATPort,
		)
	}
	return attachment, nil
}

func attachmentKey(attachment BackendAttachment) string {
	return strings.Join([]string{
		attachment.Backend,
		attachment.USBPath,
		attachment.ControlDevice,
		attachment.Interface,
	}, "|")
}
