package vowifihost

import (
	"context"
	"strings"
	"time"

	"github.com/iniwex5/vohive/pkg/logger"
	"github.com/iniwex5/vowifi-go/runtimehost"
)

const failedRuntimeStopTimeout = 10 * time.Second

func isTerminalRuntimeFailure(state runtimehost.State) bool {
	return state.SessionState == "error" && strings.TrimSpace(state.LastError) != ""
}

func (m *Manager) releaseFailedRuntime(deviceID string, inst *runtimehost.Instance, state runtimehost.State) bool {
	if m == nil || inst == nil || !m.RuntimeStore().DeleteInstance(deviceID, inst) {
		return false
	}
	m.InvalidateRuntime(deviceID, "runtime_failure")
	m.BroadcastState(deviceID)

	stopCtx, cancel := context.WithTimeout(context.Background(), failedRuntimeStopTimeout)
	defer cancel()
	if err := inst.Stop(stopCtx); err != nil {
		logger.Warn("VoWiFi 故障实例停止失败", "device", deviceID, "err", err)
	}
	if adapter := m.hostAdapter(); adapter != nil {
		adapter.RestoreSMSMode(deviceID)
	}
	logger.Error("VoWiFi 运行时故障已释放，等待目标态恢复",
		"device", deviceID,
		"class", state.LastErrorClass,
		"reason", state.LastReason,
		"err", state.LastError)
	return true
}
