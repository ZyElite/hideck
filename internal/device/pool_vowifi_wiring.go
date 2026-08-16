package device

import (
	"context"
	"fmt"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
	"github.com/yibaiba/hideck/pkg/logger"
)

// SetVoiceGateway 注入 VoWiFi 语音网关，用于优先走 IMS 外呼/挂断路径。
func (p *Pool) SetVoiceGateway(g *voicehost.Gateway) {
	p.mu.Lock()
	p.voiceGateway = g
	p.mu.Unlock()
	p.voWiFiHost().ConfigureRuntimeDependencies(g, vowifiDeliveryStore{}, poolVoWiFiRuntimeDispatcher{pool: p})
}

// GetVoiceGateway 返回绑定的 VoiceGateway 实例
func (p *Pool) GetVoiceGateway() *voicehost.Gateway {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.voiceGateway
}

// SimulateCallWithCellularData wraps voiceGW.SimulateCall with data connect
// on/off for on_demand cellular mode. If the device is in cellular mode with
// DataStrategy=on_demand, it connects data before the call and disconnects
// after. Otherwise it calls SimulateCall directly.
func (p *Pool) SimulateCallWithCellularData(
	ctx context.Context,
	deviceID string,
	req voicehost.SimulateCallRequest,
) (*voicehost.SimulateCallResult, error) {
	w := p.GetWorker(deviceID)
	needDataHook := w != nil &&
		w.Config.PhoneMode == "cellular" &&
		w.Config.DataStrategy == "on_demand"

	if needDataHook {
		nc := w.NetworkController()
		if nc != nil && !nc.IsConnected() {
			logger.Info("蜂窝 on_demand：拨号前开启数据连接", "device", deviceID)
			if err := w.StartNetwork(); err != nil {
				return nil, fmt.Errorf("蜂窝 on_demand 开启数据失败: %w", err)
			}
			waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			if err := waitForCondition(waitCtx, 500*time.Millisecond, func() bool {
				return nc.IsConnected()
			}); err != nil {
				return nil, fmt.Errorf("蜂窝 on_demand 等待数据连接超时: %w", err)
			}
		}
	}

	voiceGW := p.GetVoiceGateway()
	result, err := voiceGW.SimulateCall(ctx, deviceID, req)

	if needDataHook {
		nc := w.NetworkController()
		if nc != nil && nc.IsConnected() {
			logger.Info("蜂窝 on_demand：挂断后关闭数据连接", "device", deviceID)
			if stopErr := w.StopNetwork(); stopErr != nil {
				logger.Warn("蜂窝 on_demand 关闭数据连接失败", "device", deviceID, "err", stopErr)
			}
		}
	}

	return result, err
}
