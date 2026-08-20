package device

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
)

const voWiFiSMSSendRecoveryTimeout = 90 * time.Second

type voWiFiSMSRuntime interface {
	State() runtimehost.State
	SendSMSWithOptions(context.Context, string, string, messaging.SendOptions) (messaging.SendOutcome, error)
}

type voWiFiSMSSendRequest struct {
	DeviceID string
	To       string
	Text     string
	Options  messaging.SendOptions
	Updates  <-chan runtimehost.State
	Runtime  func() voWiFiSMSRuntime
}

func sendVoWiFiSMSWhenReady(
	ctx context.Context,
	request voWiFiSMSSendRequest,
) (messaging.SendOutcome, error) {
	lastReason := "VoWiFi 运行时尚未建立"
	for {
		runtime := currentVoWiFiSMSRuntime(request.Runtime)
		if runtime != nil {
			state := runtime.State()
			lastReason = voWiFiSMSWaitReason(state)
			if state.SMSReady || !shouldWaitForVoWiFiSMS(state) {
				outcome, err := runtime.SendSMSWithOptions(ctx, request.To, request.Text, request.Options)
				if err == nil || !errors.Is(err, messaging.ErrSMSNotReady) {
					return outcome, err
				}
				lastReason = err.Error()
				if !shouldWaitForVoWiFiSMS(runtime.State()) {
					return outcome, err
				}
			}
		}
		if err := waitForVoWiFiSMSUpdate(ctx, request.Updates); err != nil {
			return messaging.SendOutcome{}, fmt.Errorf(
				"设备 %s 的 VoWiFi 等待短信恢复失败（最后状态：%s）: %w",
				request.DeviceID, lastReason, err,
			)
		}
	}
}

func currentVoWiFiSMSRuntime(getter func() voWiFiSMSRuntime) voWiFiSMSRuntime {
	if getter == nil {
		return nil
	}
	return getter()
}

func shouldWaitForVoWiFiSMS(state runtimehost.State) bool {
	return !state.SMSReady && !strings.EqualFold(
		strings.TrimSpace(state.SMSReadyReason), "IMS SMSC is not configured",
	)
}

func voWiFiSMSWaitReason(state runtimehost.State) string {
	if reason := strings.TrimSpace(state.SMSReadyReason); reason != "" {
		return reason
	}
	if phase := strings.TrimSpace(state.Phase); phase != "" {
		return phase
	}
	return "VoWiFi 正在恢复"
}

func waitForVoWiFiSMSUpdate(ctx context.Context, updates <-chan runtimehost.State) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case _, ok := <-updates:
		if !ok {
			return errors.New("VoWiFi 状态订阅已关闭")
		}
		return nil
	}
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
