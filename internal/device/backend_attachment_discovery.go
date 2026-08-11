package device

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/config"
)

const (
	defaultBackendAttachmentPollInterval = 500 * time.Millisecond
	defaultBackendAttachmentProbeTimeout = 2 * time.Second
	defaultBackendIdentityProbeTimeout   = 5 * time.Second
	defaultMBIMIdentityProbeTimeout      = 45 * time.Second
)

// BackendAttachment is the verified runtime attachment for one managed modem.
type BackendAttachment struct {
	Backend         string `json:"backend"`
	IMEI            string `json:"imei"`
	IdentitySource  string `json:"identity_source,omitempty"`
	IdentityWarning string `json:"identity_warning,omitempty"`
	ControlDevice   string `json:"control_device"`
	Interface       string `json:"interface"`
	USBPath         string `json:"usb_path"`
	ATPort          string `json:"at_port"`
}

type BackendAttachmentQuery struct {
	IMEI                    string
	TargetBackend           string
	ATPortHint              string
	AllowATIdentityRecovery bool
}

type BackendAttachmentDiscovery struct {
	Scan                 func() ([]CompatibleModem, error)
	ProbeIdentity        func(context.Context, CompatibleModem, time.Duration) (string, error)
	Resolve              func(context.Context, CompatibleModem, time.Duration) (CompatibleModem, string)
	PollInterval         time.Duration
	ProbeTimeout         time.Duration
	IdentityProbeTimeout time.Duration
	MBIMIdentityTimeout  time.Duration
}

func NewBackendAttachmentDiscovery() BackendAttachmentDiscovery {
	return BackendAttachmentDiscovery{
		Scan:                 DiscoverCompatibleModems,
		ProbeIdentity:        ProbeCompatibleModemIdentityContext,
		Resolve:              ResolveCompatibleModemATPortContext,
		PollInterval:         defaultBackendAttachmentPollInterval,
		ProbeTimeout:         defaultBackendAttachmentProbeTimeout,
		IdentityProbeTimeout: defaultBackendIdentityProbeTimeout,
		MBIMIdentityTimeout:  defaultMBIMIdentityProbeTimeout,
	}
}

func (d BackendAttachmentDiscovery) Wait(
	ctx context.Context,
	imei string,
	targetBackend string,
) (BackendAttachment, error) {
	return d.WaitWithHint(ctx, BackendAttachmentQuery{IMEI: imei, TargetBackend: targetBackend})
}

// WaitWithHint 优先复用同一 USB 设备已有的管理口，端口改号时自动重新探测。
func (d BackendAttachmentDiscovery) WaitWithHint(
	ctx context.Context,
	query BackendAttachmentQuery,
) (BackendAttachment, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if config.NormalizeIMEI(query.IMEI) == "" {
		return BackendAttachment{}, fmt.Errorf("IMEI 无效，无法验证重枚举设备")
	}
	target, err := normalizeAttachmentBackend(query.TargetBackend)
	if err != nil {
		return BackendAttachment{}, err
	}
	query.TargetBackend = target
	if d.Scan == nil || d.ProbeIdentity == nil || d.Resolve == nil {
		return BackendAttachment{}, fmt.Errorf("设备发现器未初始化")
	}

	pollInterval := d.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultBackendAttachmentPollInterval
	}
	var lastErr error
	for {
		attachment, findErr := d.findOnce(ctx, query)
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

func (d BackendAttachmentDiscovery) findOnce(
	ctx context.Context,
	query BackendAttachmentQuery,
) (BackendAttachment, error) {
	modems, err := d.Scan()
	if err != nil {
		return BackendAttachment{}, fmt.Errorf("扫描设备失败: %w", err)
	}

	matches := make(map[string]BackendAttachment)
	var lastProbeErr error
	for _, raw := range modems {
		mode := compatibleModemBackend(raw)
		if mode != "qmi" && mode != "mbim" {
			continue
		}
		if query.TargetBackend != "" && mode != query.TargetBackend {
			continue
		}

		candidate := attachmentCandidate{
			Modem:                   raw,
			ExpectedIMEI:            query.IMEI,
			Backend:                 mode,
			ATPortHint:              query.ATPortHint,
			AllowATIdentityRecovery: query.AllowATIdentityRecovery,
		}
		attachment, matched, resolveErr := d.resolveCandidate(ctx, candidate)
		if resolveErr != nil {
			lastProbeErr = resolveErr
			continue
		}
		if !matched {
			continue
		}
		matches[attachmentKey(attachment)] = attachment
	}

	if len(matches) == 0 {
		if query.TargetBackend == "" {
			return BackendAttachment{}, attachmentNotFoundError(query.IMEI, "QMI/MBIM", lastProbeErr)
		}
		return BackendAttachment{}, attachmentNotFoundError(
			query.IMEI,
			strings.ToUpper(query.TargetBackend),
			lastProbeErr,
		)
	}
	if len(matches) > 1 {
		return BackendAttachment{}, fmt.Errorf("IMEI %s 对应到 %d 个设备路径，拒绝自动选择", query.IMEI, len(matches))
	}
	for _, attachment := range matches {
		return attachment, nil
	}
	return BackendAttachment{}, fmt.Errorf("设备发现结果为空")
}

func attachmentNotFoundError(imei, backend string, probeErr error) error {
	if probeErr == nil {
		return fmt.Errorf("未发现 IMEI %s 对应的 %s 设备", imei, backend)
	}
	return fmt.Errorf("未发现 IMEI %s 对应的 %s 设备（最后探测错误: %v）", imei, backend, probeErr)
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
