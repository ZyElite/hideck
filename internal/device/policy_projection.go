package device

import (
	"strings"
	"time"

	"github.com/yibaiba/hideck/internal/backend"
	"github.com/yibaiba/hideck/internal/cardpolicy"
	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/pkg/logger"
)

// cellularDataAllowed reports whether the software IMS path may bring up data.
// 驻网和流量分开：飞行关射频；网络只控制数据。
func cellularDataAllowed(phoneMode, dataStrategy string, networkEnabled bool) bool {
	if strings.TrimSpace(phoneMode) != "cellular" {
		return false
	}
	return dataStrategy == "always" || networkEnabled
}

func shouldSuppressCellularRadio(cfg config.DeviceConfig) bool {
	if cfg.AirplaneEnabled {
		return true
	}
	// WiFi calling 关射频。蜂窝软件电话要保持驻网，即使还没开流量。
	return cfg.VoWiFiEnabled && cfg.PhoneMode != "cellular"
}

// applyPolicyToWorker 把卡策略投影进 worker.Config 的运行时有效字段。
// 不在此触发 re-apply，仅做纯投影，便于单测。
func applyPolicyToWorker(w *Worker, p cardpolicy.Policy) {
	if w == nil {
		return
	}
	w.Config.NetworkEnabled = p.NetworkEnabled
	w.Config.VoWiFiEnabled = p.VoWiFiEnabled
	w.Config.AirplaneEnabled = p.AirplaneEnabled
	w.Config.PhoneMode = p.PhoneMode
	w.Config.DataStrategy = p.DataStrategy
	if p.VoWiFiEnabled {
		if p.PhoneMode == "cellular" {
			if p.AirplaneEnabled {
				w.Config.NetworkEnabled = false
			} else if p.DataStrategy == "always" {
				w.Config.NetworkEnabled = true
			}
		} else {
			w.Config.AirplaneEnabled = true
			w.Config.NetworkEnabled = false
		}
	}
	w.Config.IPVersion = strings.TrimSpace(p.IPVersion)
	if w.Config.IPVersion == "" {
		w.Config.IPVersion = "v4"
	}
	w.Config.APN = strings.TrimSpace(p.APN)
	w.Config.SMSEnabled = true // SMS 恒开
	w.restoreNetworkAfterVoWiFi = p.NetworkEnabled
	w.setCellularRadioSuppressed(shouldSuppressCellularRadio(w.Config))
}

type policyApplyResult struct {
	Applied bool
	ICCID   string
	Reason  string
	Err     error
}

// resolveAndApplyPolicy 解析 worker 当前 ICCID 的策略，投影并复用现有 apply 路径。
func (p *Pool) resolveAndApplyPolicy(worker *Worker, reason string) policyApplyResult {
	if p == nil || worker == nil || p.policyResolver == nil {
		return policyApplyResult{}
	}
	iccid := worker.CurrentICCID()
	if iccid == "" {
		logger.Info("跳过策略投影：ICCID 未就绪", "device", worker.ID, "reason", reason)
		return policyApplyResult{Reason: "iccid_empty"}
	}
	pol, err := p.policyResolver.Resolve(iccid)
	if err != nil {
		logger.Warn("解析卡策略失败", "device", worker.ID, "iccid", iccid, "err", err)
		return policyApplyResult{ICCID: iccid, Reason: "resolve_failed", Err: err}
	}
	applyPolicyToWorker(worker, pol)
	logger.Info("已投影卡策略", "device", worker.ID, "iccid", iccid,
		"network", pol.NetworkEnabled, "vowifi", pol.VoWiFiEnabled,
		"airplane", worker.Config.AirplaneEnabled, "reason", reason)

	// 三态分支：VoWiFi / 纯飞行 / 在线(含连网)。射频模式按策略真正切换，
	// 补齐此前“airplane 字段被投影但从不执行”的缺口。
	switch {
	case pol.AirplaneEnabled:
		// 飞行优先：蜂窝软件电话可以保持开启，只关射频和流量。
		p.enterAirplaneModeFromPolicy(worker, reason)
	case pol.VoWiFiEnabled && pol.PhoneMode == "cellular":
		// 蜂窝：射频保持在线以驻网。网络开着才连数据；on_demand 拨号时再开数据。
		p.exitAirplaneModeIfNeeded(worker, reason)
		if err := p.applyNetworkPreference(worker); err != nil {
			logger.Warn("应用网络偏好失败", "device", worker.ID, "err", err)
		}
	case pol.VoWiFiEnabled:
		// WiFi calling 原有路径：网络偏好按 false 走(停数据网)，射频由 VoWiFi 恢复流程切 RFOff。
		if err := p.applyNetworkPreference(worker); err != nil {
			logger.Warn("应用网络偏好失败", "device", worker.ID, "err", err)
		}
	default:
		// 在线待机或连网：飞行关着就驻网；网络开关只决定是否拉起数据。
		p.exitAirplaneModeIfNeeded(worker, reason)
		if err := p.applyNetworkPreference(worker); err != nil {
			logger.Warn("应用网络偏好失败", "device", worker.ID, "err", err)
		}
	}
	if pol.VoWiFiEnabled && !cellularSoftwarePhoneHeld(worker, pol) {
		p.scheduleDesiredVoWiFiRecover(worker.ID, reason, time.Now())
	} else {
		p.clearDesiredVoWiFiRecoverState(worker.ID)
	}
	return policyApplyResult{Applied: true, ICCID: iccid, Reason: reason}
}

func withConnectHoldRF(cfg config.DeviceConfig) config.DeviceConfig {
	cfg.ConnectHoldRF = true
	return cfg
}

func clearConnectHoldRF(w *Worker) {
	if w != nil {
		w.Config.ConnectHoldRF = false
	}
}

// holdRadioOffOnConnect 连接期暂扣射频：控制口刚恢复时先 RFOff，不改卡策略里的 AirplaneEnabled。
// 成功（已飞或刚切到 RFOff）后清掉 ConnectHoldRF，避免 QMI 后台重试再打一轮飞。
func (p *Pool) holdRadioOffOnConnect(w *Worker, reason string) {
	if w == nil {
		return
	}
	if reason == "" {
		reason = "connect_hold_rf"
	}
	w.setCellularRadioSuppressed(true)
	if p != nil && p.ctx != nil {
		_ = w.cancelRadioRegistrationReconcile(p.ctx, reason)
	}
	if nc := w.NetworkController(); nc != nil && nc.IsConnected() {
		_ = w.StopNetwork()
	}
	w.clearCachedIP()
	if w.Backend == nil {
		return
	}
	ctrl, ok := w.Backend.(backend.OperatingModeController)
	if !ok {
		logger.Warn("设备不支持射频控制，无法在连接期暂扣射频", "device", w.ID, "reason", reason)
		clearConnectHoldRF(w)
		return
	}
	if cur, err := ctrl.GetOperatingMode(p.ctx); err == nil && isFlightOperatingMode(cur) {
		logger.Info("连接期射频已处于飞行，保持暂扣", "device", w.ID, "reason", reason)
		clearConnectHoldRF(w)
		return
	}
	if err := ctrl.SetOperatingMode(p.ctx, backend.ModeRFOff); err != nil {
		logger.Warn("连接期暂扣射频失败", "device", w.ID, "reason", reason, "err", err)
		return
	}
	clearConnectHoldRF(w)
	logger.Info("连接期已暂扣射频", "device", w.ID, "reason", reason)
}

// applyAfterQMIControlReady QMI 后台起来后收口：身份已在则按卡策略投影，
// 不再无条件 RFOff。身份未到且仍要先飞时才 hold 一次。
func (p *Pool) applyAfterQMIControlReady(worker *Worker, reason string) {
	if p == nil || worker == nil {
		return
	}
	if worker.CurrentICCID() != "" {
		clearConnectHoldRF(worker)
		p.resolveAndApplyPolicy(worker, reason)
		return
	}
	if worker.Config.ConnectHoldRF {
		p.holdRadioOffOnConnect(worker, "connect_hold_rf")
	}
	if err := p.applyNetworkPreference(worker); err != nil {
		logger.Warn("QMI 控制面就绪后应用网络偏好失败", "device", worker.ID, "reason", reason, "err", err)
	}
}

// enterAirplaneModeFromPolicy 按策略进入纯飞行：先断数据网，再把射频切到 RFOff。
// 已处于飞行则跳过。设备不支持射频控制时仅告警。
func (p *Pool) enterAirplaneModeFromPolicy(w *Worker, reason string) {
	if w == nil {
		return
	}
	w.setCellularRadioSuppressed(true)
	if p.ctx != nil {
		_ = w.cancelRadioRegistrationReconcile(p.ctx, reason)
	}
	if nc := w.NetworkController(); nc != nil && nc.IsConnected() {
		_ = w.StopNetwork()
	}
	w.clearCachedIP()
	ctrl, ok := w.Backend.(backend.OperatingModeController)
	if !ok {
		logger.Warn("设备不支持射频控制，无法投影飞行模式", "device", w.ID, "reason", reason)
		return
	}
	if cur, err := ctrl.GetOperatingMode(p.ctx); err == nil && isFlightOperatingMode(cur) {
		return
	}
	if err := ctrl.SetOperatingMode(p.ctx, backend.ModeRFOff); err != nil {
		logger.Warn("投影飞行模式失败", "device", w.ID, "reason", reason, "err", err)
		return
	}
	logger.Info("已按策略进入飞行模式", "device", w.ID, "reason", reason)
}

// exitAirplaneModeIfNeeded 当设备当前处于飞行(RFOff/LowPower)且策略不要求飞行时，切回 Online。
func (p *Pool) exitAirplaneModeIfNeeded(w *Worker, reason string) {
	if w == nil {
		return
	}
	ctrl, ok := w.Backend.(backend.OperatingModeController)
	if !ok {
		return
	}
	cur, err := ctrl.GetOperatingMode(p.ctx)
	if err != nil || !isFlightOperatingMode(cur) {
		return
	}
	if err := ctrl.SetOperatingMode(p.ctx, backend.ModeOnline); err != nil {
		logger.Warn("退出飞行模式失败", "device", w.ID, "reason", reason, "err", err)
		return
	}
	logger.Info("已按策略退出飞行模式", "device", w.ID, "reason", reason)
}

// CurrentICCIDForDevice 返回指定设备当前 worker 的 ICCID（无 worker 或未就绪返回空串）。
func (p *Pool) CurrentICCIDForDevice(deviceID string) string {
	if p == nil {
		return ""
	}
	w := p.GetWorker(deviceID)
	if w == nil {
		return ""
	}
	return w.CurrentICCID()
}

// SetPolicyResolver 注入卡策略解析器（cmd/hideck 启动时调用）。
func (p *Pool) SetPolicyResolver(r cardpolicy.Resolver) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.policyResolver = r
	p.mu.Unlock()
}
