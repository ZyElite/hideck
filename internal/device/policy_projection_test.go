package device

import (
	"context"
	"testing"
	"time"

	"github.com/yibaiba/hideck/internal/backend"
	"github.com/yibaiba/hideck/internal/cardpolicy"
	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/vowifihost"
)

func TestApplyPolicyCellularAirplaneKeepsSoftwarePhone(t *testing.T) {
	w := &Worker{ID: "wwan0"}
	applyPolicyToWorker(w, cardpolicy.Policy{
		ICCID: "x", VoWiFiEnabled: true, PhoneMode: "cellular",
		AirplaneEnabled: true, NetworkEnabled: true, DataStrategy: "on_demand",
	})
	if !w.Config.AirplaneEnabled || !w.Config.VoWiFiEnabled || w.Config.NetworkEnabled {
		t.Fatalf("蜂窝飞行应保留软件电话并关流量: %+v", w.Config)
	}
	if !w.cellularRadioIsSuppressed() {
		t.Fatal("蜂窝飞行应抑制射频")
	}
}

func TestApplyPolicyCellularIdleKeepsCamped(t *testing.T) {
	w := &Worker{ID: "wwan0"}
	applyPolicyToWorker(w, cardpolicy.Policy{
		ICCID: "x", VoWiFiEnabled: true, PhoneMode: "cellular", DataStrategy: "on_demand",
	})
	if w.Config.AirplaneEnabled || w.Config.NetworkEnabled || w.cellularRadioIsSuppressed() {
		t.Fatalf("蜂窝未开流量应保持驻网: %+v suppressed=%v", w.Config, w.cellularRadioIsSuppressed())
	}

	applyPolicyToWorker(w, cardpolicy.Policy{
		ICCID: "x", VoWiFiEnabled: true, PhoneMode: "cellular", DataStrategy: "on_demand", NetworkEnabled: true,
	})
	if w.Config.AirplaneEnabled || !w.Config.NetworkEnabled || w.cellularRadioIsSuppressed() {
		t.Fatalf("打开网络后应保持在线并允许数据: %+v suppressed=%v", w.Config, w.cellularRadioIsSuppressed())
	}
}

func TestApplyPolicyProjectsFields(t *testing.T) {
	w := &Worker{ID: "wwan0"}
	applyPolicyToWorker(w, cardpolicy.Policy{
		ICCID: "x", NetworkEnabled: true, VoWiFiEnabled: true,
		AirplaneEnabled: true, IPVersion: "v4v6", APN: "ims",
	})
	if w.Config.NetworkEnabled || !w.Config.VoWiFiEnabled || !w.Config.AirplaneEnabled {
		t.Fatalf("WiFi calling 投影应关流量并保持飞行: %+v", w.Config)
	}
	if w.Config.IPVersion != "v4v6" || w.Config.APN != "ims" {
		t.Fatalf("ip/apn 未投影: %+v", w.Config)
	}
	if !w.Config.SMSEnabled {
		t.Fatal("SMS 应恒为 true")
	}
	if !w.cellularRadioIsSuppressed() {
		t.Fatal("VoWiFi/飞行策略必须抑制蜂窝射频协调")
	}
}

func TestInitialCellularRadioSuppressionWaitsForLiveCardPolicy(t *testing.T) {
	p := NewPool(nil)
	defer p.cancel()
	p.SetPolicyResolver(&stubPolicyResolver{})

	if !p.initialCellularRadioSuppression(config.DeviceConfig{}) {
		t.Fatal("存在卡策略解析器时，真实 ICCID 策略投影前必须抑制蜂窝驻网")
	}

	w := &Worker{ID: "wwan0"}
	w.setCellularRadioSuppressed(p.initialCellularRadioSuppression(w.Config))
	applyPolicyToWorker(w, cardpolicy.Policy{ICCID: "123", NetworkEnabled: true})
	if w.cellularRadioIsSuppressed() {
		t.Fatal("在线卡策略投影后必须解除蜂窝驻网抑制")
	}
}

func TestInitialCellularRadioSuppressionWithoutResolverUsesDeviceConfig(t *testing.T) {
	p := NewPool(nil)
	defer p.cancel()

	if p.initialCellularRadioSuppression(config.DeviceConfig{}) {
		t.Fatal("无卡策略解析器且静态策略在线时不应抑制蜂窝驻网")
	}
	if !p.initialCellularRadioSuppression(config.DeviceConfig{VoWiFiEnabled: true}) {
		t.Fatal("静态 VoWiFi 策略必须抑制蜂窝驻网")
	}
}

func TestHoldRadioOffOnConnectDoesNotChangeStoredAirplane(t *testing.T) {
	p := &Pool{ctx: context.Background()}
	stub := &workerStatusBackendStub{opMode: backend.ModeOnline}
	w := &Worker{
		ID:      "wwan0",
		Backend: stub,
		Config:  config.DeviceConfig{AirplaneEnabled: false, ConnectHoldRF: true},
	}

	p.holdRadioOffOnConnect(w, "connect_hold_rf")

	if w.Config.AirplaneEnabled {
		t.Fatal("连接期先飞不得改写 AirplaneEnabled")
	}
	if !w.cellularRadioIsSuppressed() {
		t.Fatal("连接期先飞应暂扣驻网协调")
	}
	if len(stub.setOpModeCalls) != 1 || stub.setOpModeCalls[0] != backend.ModeRFOff {
		t.Fatalf("连接期第一条射频指令必须是 RFOff: %+v", stub.setOpModeCalls)
	}
	if w.Config.ConnectHoldRF {
		t.Fatal("成功 hold 后应清掉 ConnectHoldRF，避免 QMI 重试再飞")
	}
}

func TestHoldRadioOffOnConnectIdempotentWhenAlreadyFlight(t *testing.T) {
	p := &Pool{ctx: context.Background()}
	stub := &workerStatusBackendStub{opMode: backend.ModeRFOff}
	w := &Worker{ID: "wwan0", Backend: stub}

	w.Config.ConnectHoldRF = true
	p.holdRadioOffOnConnect(w, "connect_hold_rf")

	if len(stub.setOpModeCalls) != 0 {
		t.Fatalf("已在飞行不应重复切: %+v", stub.setOpModeCalls)
	}
	if w.Config.ConnectHoldRF {
		t.Fatal("已在飞行的 hold 也应清掉 ConnectHoldRF")
	}
}

func TestWithConnectHoldRFDoesNotPersistIntent(t *testing.T) {
	cfg := withConnectHoldRF(config.DeviceConfig{ID: "wwan0", AirplaneEnabled: false})
	if !cfg.ConnectHoldRF {
		t.Fatal("withConnectHoldRF 应打上运行时标记")
	}
	if cfg.AirplaneEnabled {
		t.Fatal("withConnectHoldRF 不得改飞行策略")
	}
}

func TestIdentityReadyAfterConnectHoldRestoresCamp(t *testing.T) {
	p := &Pool{ctx: context.Background()}
	p.SetPolicyResolver(&stubPolicyResolver{
		pol: cardpolicy.Policy{ICCID: "123", AirplaneEnabled: false, NetworkEnabled: false},
	})
	stub := &workerStatusBackendStub{opMode: backend.ModeOnline}
	w := &Worker{
		ID:      "wwan0",
		Backend: stub,
		Config:  config.DeviceConfig{AirplaneEnabled: false, ConnectHoldRF: true},
	}
	w.state.Identity.ICCID = "123"

	p.holdRadioOffOnConnect(w, "connect_hold_rf")
	res := p.resolveAndApplyPolicy(w, "identity_ready")
	if !res.Applied {
		t.Fatalf("identity_ready 应应用策略: %+v", res)
	}
	if w.Config.AirplaneEnabled {
		t.Fatal("老卡驻网策略不得被写成飞行")
	}
	if len(stub.setOpModeCalls) < 2 || stub.setOpModeCalls[0] != backend.ModeRFOff || stub.setOpModeCalls[1] != backend.ModeOnline {
		t.Fatalf("先飞再按策略 Online: %+v", stub.setOpModeCalls)
	}
	if w.cellularRadioIsSuppressed() {
		t.Fatal("驻网策略投影后应解除射频抑制")
	}

	before := append([]backend.OperatingMode(nil), stub.setOpModeCalls...)
	w.Config.ConnectHoldRF = true
	p.applyAfterQMIControlReady(w, "qmi_core_recovered")
	if w.cellularRadioIsSuppressed() {
		t.Fatal("QMI 恢复时身份已在，不得再次抑制驻网")
	}
	if w.Config.AirplaneEnabled {
		t.Fatal("QMI 恢复不得把驻网卡写成飞行")
	}
	if w.Config.ConnectHoldRF {
		t.Fatal("身份已在时 QMI 恢复应清掉 ConnectHoldRF")
	}
	for i := len(before); i < len(stub.setOpModeCalls); i++ {
		if stub.setOpModeCalls[i] == backend.ModeRFOff {
			t.Fatalf("QMI 恢复不得再 RFOff: %+v", stub.setOpModeCalls)
		}
	}
}

func TestIdentityReadyAfterConnectHoldKeepsAirplaneOrVoWiFiRFOff(t *testing.T) {
	cases := []struct {
		name string
		pol  cardpolicy.Policy
	}{
		{name: "airplane", pol: cardpolicy.Policy{ICCID: "123", AirplaneEnabled: true}},
		{name: "vowifi-wifi", pol: cardpolicy.Policy{ICCID: "123", VoWiFiEnabled: true, PhoneMode: "wifi"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Pool{ctx: context.Background()}
			p.SetPolicyResolver(&stubPolicyResolver{pol: tc.pol})
			stub := &workerStatusBackendStub{opMode: backend.ModeOnline}
			w := &Worker{
				ID:      "wwan0",
				Backend: stub,
				Config:  config.DeviceConfig{AirplaneEnabled: false, ConnectHoldRF: true},
			}
			w.state.Identity.ICCID = "123"

			p.holdRadioOffOnConnect(w, "connect_hold_rf")
			res := p.resolveAndApplyPolicy(w, "identity_ready")
			if !res.Applied {
				t.Fatalf("identity_ready 应应用策略: %+v", res)
			}
			for _, mode := range stub.setOpModeCalls {
				if mode == backend.ModeOnline {
					t.Fatalf("%s 策略投影后不应 Online: %+v", tc.name, stub.setOpModeCalls)
				}
			}
			if !w.cellularRadioIsSuppressed() {
				t.Fatalf("%s 应保持射频抑制", tc.name)
			}
		})
	}
}

// 投影时按策略真正进入飞行模式：当前在线 ⇒ 切 RFOff。
func TestProjectionEntersAirplaneMode(t *testing.T) {
	p := &Pool{ctx: context.Background()}
	stub := &workerStatusBackendStub{opMode: backend.ModeOnline}
	w := &Worker{ID: "wwan0", Backend: stub}

	p.enterAirplaneModeFromPolicy(w, "test")

	if len(stub.setOpModeCalls) != 1 || stub.setOpModeCalls[0] != backend.ModeRFOff {
		t.Fatalf("应切到 RFOff: %+v", stub.setOpModeCalls)
	}
}

// 幂等：已在飞行模式时不重复下发 SetOperatingMode。
func TestProjectionEnterAirplaneIdempotent(t *testing.T) {
	p := &Pool{ctx: context.Background()}
	stub := &workerStatusBackendStub{opMode: backend.ModeRFOff}
	w := &Worker{ID: "wwan0", Backend: stub}

	p.enterAirplaneModeFromPolicy(w, "test")

	if len(stub.setOpModeCalls) != 0 {
		t.Fatalf("已在飞行不应重复切: %+v", stub.setOpModeCalls)
	}
}

// 投影时按策略退出飞行：当前 RFOff 且策略不要求飞行 ⇒ 切回 Online。
func TestProjectionExitsAirplaneMode(t *testing.T) {
	p := &Pool{ctx: context.Background()}
	stub := &workerStatusBackendStub{opMode: backend.ModeRFOff}
	w := &Worker{ID: "wwan0", Backend: stub}

	p.exitAirplaneModeIfNeeded(w, "test")

	if len(stub.setOpModeCalls) != 1 || stub.setOpModeCalls[0] != backend.ModeOnline {
		t.Fatalf("应切回 Online: %+v", stub.setOpModeCalls)
	}
}

// 已在线时退出飞行是 no-op。
func TestProjectionExitAirplaneSkipsWhenOnline(t *testing.T) {
	p := &Pool{ctx: context.Background()}
	stub := &workerStatusBackendStub{opMode: backend.ModeOnline}
	w := &Worker{ID: "wwan0", Backend: stub}

	p.exitAirplaneModeIfNeeded(w, "test")

	if len(stub.setOpModeCalls) != 0 {
		t.Fatalf("已在线不应切: %+v", stub.setOpModeCalls)
	}
}

type stubPolicyResolver struct {
	pol cardpolicy.Policy
	err error
}

func (s *stubPolicyResolver) Resolve(iccid string) (cardpolicy.Policy, error) {
	return s.pol, s.err
}

func TestResolveAndApplyPolicy_EmptyICCID(t *testing.T) {
	p := &Pool{}
	w := &Worker{ID: "wwan0"}
	p.SetPolicyResolver(&stubPolicyResolver{})

	res := p.resolveAndApplyPolicy(w, "test")
	if res.Applied || res.Reason != "iccid_empty" {
		t.Fatalf("空 ICCID 应返回 iccid_empty: %+v", res)
	}
}

func TestResolveAndApplyPolicyNetworkOffKeepsCamped(t *testing.T) {
	p := &Pool{ctx: context.Background()}
	p.SetPolicyResolver(&stubPolicyResolver{
		pol: cardpolicy.Policy{ICCID: "123", VoWiFiEnabled: false, AirplaneEnabled: false, NetworkEnabled: false},
	})
	stub := &workerStatusBackendStub{opMode: backend.ModeOnline}
	w := &Worker{ID: "wwan0", Backend: stub}
	w.state.Identity.ICCID = "123"

	res := p.resolveAndApplyPolicy(w, "vowifi_disabled")
	if !res.Applied {
		t.Fatalf("应成功应用: %+v", res)
	}
	for _, mode := range stub.setOpModeCalls {
		if mode == backend.ModeRFOff {
			t.Fatalf("网络关闭应保持驻网，不应切入飞行: %+v", stub.setOpModeCalls)
		}
	}
	if w.cellularRadioIsSuppressed() {
		t.Fatal("网络关闭仍应允许驻网")
	}
}

func TestResolveAndApplyPolicy_ResolvesAndProjects(t *testing.T) {
	p := &Pool{ctx: context.Background()}
	p.SetPolicyResolver(&stubPolicyResolver{
		pol: cardpolicy.Policy{ICCID: "123", AirplaneEnabled: true},
	})
	stub := &workerStatusBackendStub{opMode: backend.ModeOnline}
	w := &Worker{ID: "wwan0", Backend: stub}
	w.state.Identity.ICCID = "123"

	res := p.resolveAndApplyPolicy(w, "test")
	if !res.Applied {
		t.Fatalf("应成功应用: %+v", res)
	}
	if !w.Config.AirplaneEnabled {
		t.Fatal("策略投影失败")
	}
	if len(stub.setOpModeCalls) != 1 || stub.setOpModeCalls[0] != backend.ModeRFOff {
		t.Fatalf("应切入飞行模式: %+v", stub.setOpModeCalls)
	}
}

func TestResolveAndApplyPolicyDoesNotRecoverVoWiFiWhenCardPolicyDisabled(t *testing.T) {
	p := NewPool(nil)
	defer p.cancel()
	p.SetPolicyResolver(&stubPolicyResolver{
		pol: cardpolicy.Policy{ICCID: "123", VoWiFiEnabled: false},
	})
	commands := make(chan vowifihost.LifecycleCommand, 1)
	p.voWiFiHost().LifecycleControllerForTest().TestRun = func(ctx context.Context, cmd vowifihost.LifecycleCommand) error {
		commands <- cmd
		return nil
	}
	w := &Worker{ID: "wwan0"}
	w.state.Identity.ICCID = "123"
	w.state.Identity.IMSI = "001010000000001"

	res := p.resolveAndApplyPolicy(w, "test")
	if !res.Applied {
		t.Fatalf("应成功应用: %+v", res)
	}

	select {
	case cmd := <-commands:
		t.Fatalf("卡策略未开启 VoWiFi 时不应调度恢复: %+v", cmd)
	case <-time.After(120 * time.Millisecond):
	}
}

func TestResolveAndApplyPolicyDoesNotRecoverCellularOnDemand(t *testing.T) {
	p := NewPool(nil)
	defer p.cancel()
	p.SetPolicyResolver(&stubPolicyResolver{
		pol: cardpolicy.Policy{
			ICCID:         "123",
			VoWiFiEnabled: true,
			PhoneMode:     "cellular",
			DataStrategy:  "on_demand",
		},
	})
	commands := make(chan vowifihost.LifecycleCommand, 1)
	p.voWiFiHost().LifecycleControllerForTest().TestRun = func(ctx context.Context, cmd vowifihost.LifecycleCommand) error {
		commands <- cmd
		return nil
	}
	w := &Worker{ID: "wwan0"}
	w.state.Identity.ICCID = "123"
	w.state.Identity.IMSI = "001010000000001"

	res := p.resolveAndApplyPolicy(w, "startup_post_apply")
	if !res.Applied {
		t.Fatalf("应成功应用: %+v", res)
	}
	if w.Config.PhoneMode != "cellular" || w.Config.AirplaneEnabled {
		t.Fatalf("蜂窝 on_demand 未开流量应保持驻网: %+v", w.Config)
	}
	if w.cellularRadioIsSuppressed() {
		t.Fatal("蜂窝 on_demand 未开流量仍应允许驻网")
	}

	select {
	case cmd := <-commands:
		t.Fatalf("蜂窝 on_demand 待机不应调度 VoWiFi 恢复: %+v", cmd)
	case <-time.After(120 * time.Millisecond):
	}
}

func TestResolveAndApplyPolicyDoesNotRecoverCellularAirplane(t *testing.T) {
	p := NewPool(nil)
	defer p.cancel()
	p.SetPolicyResolver(&stubPolicyResolver{
		pol: cardpolicy.Policy{
			ICCID:           "123",
			VoWiFiEnabled:   true,
			PhoneMode:       "cellular",
			DataStrategy:    "always",
			AirplaneEnabled: true,
			NetworkEnabled:  true,
		},
	})
	commands := make(chan vowifihost.LifecycleCommand, 1)
	p.voWiFiHost().LifecycleControllerForTest().TestRun = func(ctx context.Context, cmd vowifihost.LifecycleCommand) error {
		commands <- cmd
		return nil
	}
	w := &Worker{ID: "wwan0"}
	w.state.Identity.ICCID = "123"
	w.state.Identity.IMSI = "001010000000001"

	res := p.resolveAndApplyPolicy(w, "flight_mode_change")
	if !res.Applied {
		t.Fatalf("应成功应用: %+v", res)
	}
	if !w.Config.AirplaneEnabled || w.Config.NetworkEnabled || !w.Config.VoWiFiEnabled {
		t.Fatalf("蜂窝飞行应保留软件电话并关流量: %+v", w.Config)
	}

	select {
	case cmd := <-commands:
		t.Fatalf("蜂窝飞行不应调度 VoWiFi 恢复: %+v", cmd)
	case <-time.After(120 * time.Millisecond):
	}
}

func TestRefreshIdentityAndApplyCardPolicyDoesNotFallbackToConfiguredNetworkWithoutResolver(t *testing.T) {
	p := NewPool(nil)
	defer p.cancel()

	ctrl := &fakeController{}
	w := &Worker{
		ID: "wwan0",
		Config: config.DeviceConfig{
			ID:             "wwan0",
			NetworkEnabled: true,
		},
		Backend: &workerStartupIdentityBackendStub{
			liveICCID: "898600000000000001",
			liveIMSI:  "460001234567890",
		},
		netOverride: ctrl,
	}

	result, err := p.refreshIdentityAndApplyCardPolicy(w, "startup_post_apply")
	if err != nil {
		t.Fatalf("refreshIdentityAndApplyCardPolicy() error=%v", err)
	}
	if result.ICCID != "898600000000000001" || result.IMSI != "460001234567890" {
		t.Fatalf("live identity result mismatch: %+v", result)
	}
	if ctrl.connected {
		t.Fatal("无 policy resolver 时不应回退到旧 worker.Config 连接数据网络")
	}
}
