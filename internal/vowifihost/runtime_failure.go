package vowifihost

import (
	"context"
	"strings"
	"time"

	"github.com/iniwex5/vohive/pkg/logger"
	"github.com/iniwex5/vowifi-go/runtimehost"
)

const failedRuntimeStopTimeout = 10 * time.Second

const ikeReauthenticationReason = "ike_reauthentication"

func isTerminalRuntimeFailure(state runtimehost.State) bool {
	return state.SessionState == "error" && strings.TrimSpace(state.LastError) != ""
}

func (m *Manager) releaseFailedRuntime(deviceID string, inst *runtimehost.Instance, state runtimehost.State) bool {
	controlledReauth := state.LastErrorClass == runtimehost.ErrorClassReauthentication
	if m == nil || inst == nil || !m.RuntimeStore().DeleteInstance(deviceID, inst) {
		return false
	}
	invalidationReason := "runtime_failure"
	if controlledReauth {
		invalidationReason = ikeReauthenticationReason
	}
	m.InvalidateRuntime(deviceID, invalidationReason)
	m.BroadcastState(deviceID)

	stopCtx, cancel := context.WithTimeout(context.Background(), failedRuntimeStopTimeout)
	defer cancel()
	if err := inst.Stop(stopCtx); err != nil {
		logger.Warn("VoWiFi 故障实例停止失败", "device", deviceID, "err", err)
	}
	if adapter := m.hostAdapter(); adapter != nil {
		adapter.RestoreSMSMode(deviceID)
	}
	if controlledReauth {
		logger.Info("VoWiFi IKE 重鉴权旧实例已释放，立即请求新运行时", "device", deviceID)
		m.requestRuntimeRecycle(deviceID, ikeReauthenticationReason)
		return true
	}
	logger.Error("VoWiFi 运行时故障已释放，等待目标态恢复",
		"device", deviceID,
		"class", state.LastErrorClass,
		"reason", state.LastReason,
		"err", state.LastError)
	return true
}
