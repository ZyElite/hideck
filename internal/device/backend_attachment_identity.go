package device

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yibaiba/hideck/internal/config"
)

type attachmentCandidate struct {
	Modem                   CompatibleModem
	ExpectedIMEI            string
	Backend                 string
	ATPortHint              string
	AllowATIdentityRecovery bool
}

func (d BackendAttachmentDiscovery) resolveCandidate(
	ctx context.Context,
	candidate attachmentCandidate,
) (BackendAttachment, bool, error) {
	raw := candidate.Modem
	probedIMEI, err := d.ProbeIdentity(ctx, raw, d.identityProbeTimeout(candidate.Backend))
	if err != nil {
		if candidate.AllowATIdentityRecovery {
			return d.resolveCandidateViaAT(ctx, candidate, err)
		}
		return BackendAttachment{}, false, fmt.Errorf("%s 身份探测失败: %w", raw.ControlPath, err)
	}
	if !config.IMEIMatches(candidate.ExpectedIMEI, probedIMEI) {
		return BackendAttachment{}, false, nil
	}
	return d.attachmentFromProtocolIdentity(ctx, candidate, probedIMEI)
}

func (d BackendAttachmentDiscovery) attachmentFromProtocolIdentity(
	ctx context.Context,
	candidate attachmentCandidate,
	probedIMEI string,
) (BackendAttachment, bool, error) {
	resolved := candidate.Modem
	if modemOwnsATPort(resolved, candidate.ATPortHint) {
		resolved.ATPort = strings.TrimSpace(candidate.ATPortHint)
	} else {
		resolved, _ = d.Resolve(ctx, resolved, d.probeTimeout())
	}
	if ctx.Err() != nil {
		return BackendAttachment{}, false, ctx.Err()
	}
	attachment, err := attachmentFromCompatibleModem(resolved, strings.TrimSpace(probedIMEI), candidate.Backend)
	attachment.IdentitySource = candidate.Backend
	return attachment, err == nil, err
}

func (d BackendAttachmentDiscovery) resolveCandidateViaAT(
	ctx context.Context,
	candidate attachmentCandidate,
	identityErr error,
) (BackendAttachment, bool, error) {
	raw := candidate.Modem
	if modemOwnsATPort(raw, candidate.ATPortHint) {
		raw.ATPort = strings.TrimSpace(candidate.ATPortHint)
	}
	resolved, probedIMEI := d.Resolve(ctx, raw, d.probeTimeout())
	if ctx.Err() != nil {
		return BackendAttachment{}, false, ctx.Err()
	}
	if !config.IMEIMatches(candidate.ExpectedIMEI, probedIMEI) {
		return BackendAttachment{}, false, fmt.Errorf(
			"%s 控制协议身份探测失败且 AT 恢复未匹配 IMEI: %v",
			raw.ControlPath,
			identityErr,
		)
	}
	attachment, err := attachmentFromCompatibleModem(resolved, strings.TrimSpace(probedIMEI), candidate.Backend)
	attachment.IdentitySource = "at_recovery"
	attachment.IdentityWarning = identityErr.Error()
	return attachment, err == nil, err
}

func ProbeCompatibleModemIdentityContext(
	ctx context.Context,
	modem CompatibleModem,
	timeout time.Duration,
) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = defaultBackendAttachmentProbeTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	switch compatibleModemBackend(modem) {
	case "qmi":
		return ProbeIMEIViaQMIContext(probeCtx, modem.ControlPath)
	case "mbim":
		return ProbeIMEIViaMBIMContext(probeCtx, modem.ControlPath)
	default:
		return "", fmt.Errorf("设备 %s 没有可用 QMI/MBIM 身份协议", modem.ControlPath)
	}
}

func compatibleModemBackend(modem CompatibleModem) string {
	backend := strings.ToLower(strings.TrimSpace(modem.TransportType))
	if backend == "" {
		backend = strings.ToLower(strings.TrimSpace(modem.Mode))
	}
	return backend
}

func modemOwnsATPort(modem CompatibleModem, atPort string) bool {
	atPort = strings.TrimSpace(atPort)
	if atPort == "" {
		return false
	}
	return strings.TrimSpace(modem.ATPort) == atPort || containsPort(modem.ATPorts, atPort)
}

func (d BackendAttachmentDiscovery) probeTimeout() time.Duration {
	if d.ProbeTimeout > 0 {
		return d.ProbeTimeout
	}
	return defaultBackendAttachmentProbeTimeout
}

func (d BackendAttachmentDiscovery) identityProbeTimeout(backend string) time.Duration {
	if strings.EqualFold(strings.TrimSpace(backend), "mbim") {
		if d.MBIMIdentityTimeout > 0 {
			return d.MBIMIdentityTimeout
		}
		return defaultMBIMIdentityProbeTimeout
	}
	if d.IdentityProbeTimeout > 0 {
		return d.IdentityProbeTimeout
	}
	return defaultBackendIdentityProbeTimeout
}
