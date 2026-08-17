package device

import (
	"context"
	"strings"
	"testing"

	"github.com/yibaiba/hideck/internal/config"
)

func newPoolWithWorkerForSync(id string, cfg config.DeviceConfig) (*Pool, *Worker) {
	p := &Pool{workers: map[string]*Worker{}}
	w := &Worker{ID: id, Config: cfg}
	p.workers[id] = w
	return p, w
}

// 开 VoWiFi：同步 w.Config 为 vowifi=T、airplane=T、network=F（否则概览仍显示蜂窝面板）。
// 关 VoWiFi：仅清 vowifi，不在此清 airplane——airplane 是用户飞行意图，交由
// resolveAndApplyPolicy 按卡策略重投影回退。
func TestSetWorkerVoWiFiPolicySyncsConfig(t *testing.T) {
	p, w := newPoolWithWorkerForSync("wwan0", config.DeviceConfig{NetworkEnabled: true})

	p.SetWorkerVoWiFiPolicy("wwan0", true)
	if !w.Config.VoWiFiEnabled || !w.Config.AirplaneEnabled || w.Config.NetworkEnabled {
		t.Fatalf("开 WiFi calling 应 vowifi=T airplane=T network=F: %+v", w.Config)
	}
	if !w.cellularRadioIsSuppressed() {
		t.Fatal("开 vowifi 应抑制蜂窝射频协调")
	}

	p.SetWorkerVoWiFiPolicy("wwan0", false)
	if w.Config.VoWiFiEnabled {
		t.Fatalf("关 vowifi 应清 vowifi=F: %+v", w.Config)
	}
}

// 开飞行：同步 airplane=T、vowifi=F、network=F；关飞行仅清 airplane。
func TestSetWorkerVoWiFiPolicyCellularKeepsExistingNetwork(t *testing.T) {
	p, w := newPoolWithWorkerForSync("wwan0", config.DeviceConfig{
		NetworkEnabled: true,
		PhoneMode:      "cellular",
		DataStrategy:   "on_demand",
	})

	p.SetWorkerVoWiFiPolicy("wwan0", true)
	if !w.Config.VoWiFiEnabled || w.Config.AirplaneEnabled || !w.Config.NetworkEnabled {
		t.Fatalf("蜂窝开软件电话应保留已开的数据: %+v", w.Config)
	}
}

func TestSetWorkerVoWiFiPolicyCellularIdleStaysAirplane(t *testing.T) {
	p, w := newPoolWithWorkerForSync("wwan0", config.DeviceConfig{
		PhoneMode:    "cellular",
		DataStrategy: "on_demand",
	})

	p.SetWorkerVoWiFiPolicy("wwan0", true)
	if !w.Config.VoWiFiEnabled || w.Config.AirplaneEnabled || w.Config.NetworkEnabled {
		t.Fatalf("蜂窝未开流量应保持驻网: %+v", w.Config)
	}
	if w.cellularRadioIsSuppressed() {
		t.Fatal("蜂窝未开流量仍应允许驻网")
	}
}

func TestSetWorkerAirplanePolicyKeepsCellularSoftwarePhone(t *testing.T) {
	p, w := newPoolWithWorkerForSync("wwan0", config.DeviceConfig{
		VoWiFiEnabled:  true,
		NetworkEnabled: true,
		PhoneMode:      "cellular",
	})

	p.SetWorkerAirplanePolicy("wwan0", true)
	if !w.Config.AirplaneEnabled || !w.Config.VoWiFiEnabled || w.Config.NetworkEnabled {
		t.Fatalf("蜂窝开飞行应保留软件电话并关流量: %+v", w.Config)
	}
	if !w.cellularRadioIsSuppressed() {
		t.Fatal("蜂窝开飞行应抑制射频")
	}

	p.SetWorkerAirplanePolicy("wwan0", false)
	if w.Config.AirplaneEnabled || !w.Config.VoWiFiEnabled {
		t.Fatalf("关飞行应保留软件电话: %+v", w.Config)
	}
}

func TestSetWorkerAirplanePolicyRestoresAlwaysNetwork(t *testing.T) {
	p, w := newPoolWithWorkerForSync("wwan0", config.DeviceConfig{
		VoWiFiEnabled:   true,
		PhoneMode:       "cellular",
		DataStrategy:    "always",
		AirplaneEnabled: true,
		NetworkEnabled:  false,
	})

	p.SetWorkerAirplanePolicy("wwan0", false)
	if w.Config.AirplaneEnabled || !w.Config.NetworkEnabled || !w.Config.VoWiFiEnabled {
		t.Fatalf("蜂窝 always 关飞行应写回网络: %+v", w.Config)
	}
}

func TestPrepareCellularCallRefusesAirplane(t *testing.T) {
	p, _ := newPoolWithWorkerForSync("wwan0", config.DeviceConfig{
		PhoneMode:       "cellular",
		AirplaneEnabled: true,
	})
	err := p.PrepareCellularCall(context.Background(), "wwan0")
	if err == nil || !strings.Contains(err.Error(), "飞行模式") {
		t.Fatalf("飞行中拨号应失败: %v", err)
	}
}

func TestEnsureCellularDataRefusesAirplane(t *testing.T) {
	p, _ := newPoolWithWorkerForSync("wwan0", config.DeviceConfig{
		PhoneMode:       "cellular",
		DataStrategy:    "always",
		AirplaneEnabled: true,
	})
	err := p.EnsureCellularData(context.Background(), "wwan0")
	if err == nil || !strings.Contains(err.Error(), "飞行模式") {
		t.Fatalf("飞行中开数据应失败: %v", err)
	}
}

func TestSetWorkerAirplanePolicySyncsConfig(t *testing.T) {
	p, w := newPoolWithWorkerForSync("wwan0", config.DeviceConfig{VoWiFiEnabled: true, NetworkEnabled: true})

	p.SetWorkerAirplanePolicy("wwan0", true)
	if !w.Config.AirplaneEnabled || w.Config.VoWiFiEnabled || w.Config.NetworkEnabled {
		t.Fatalf("开飞行应 airplane=T vowifi=F network=F: %+v", w.Config)
	}

	p.SetWorkerAirplanePolicy("wwan0", false)
	if w.Config.AirplaneEnabled {
		t.Fatalf("关飞行应 airplane=F: %+v", w.Config)
	}
}

// 开网络：互斥关 vowifi/airplane，并同步 ip/apn。
func TestSetWorkerNetworkPolicyMutualExclusion(t *testing.T) {
	p, w := newPoolWithWorkerForSync("wwan0", config.DeviceConfig{VoWiFiEnabled: true, AirplaneEnabled: true})

	p.SetWorkerNetworkPolicy("wwan0", true, "v4v6", "ims")
	if !w.Config.NetworkEnabled || w.Config.VoWiFiEnabled || w.Config.AirplaneEnabled {
		t.Fatalf("开网络应互斥关 vowifi/airplane: %+v", w.Config)
	}
	if w.cellularRadioIsSuppressed() {
		t.Fatal("开网络应解除蜂窝射频抑制")
	}
	if w.Config.IPVersion != "v4v6" || w.Config.APN != "ims" {
		t.Fatalf("ip/apn 应同步: %+v", w.Config)
	}
}

func TestSetWorkerNetworkPolicyKeepsCellularSoftwarePhone(t *testing.T) {
	p, w := newPoolWithWorkerForSync("wwan0", config.DeviceConfig{
		VoWiFiEnabled: true,
		PhoneMode:     "cellular",
		DataStrategy:  "on_demand",
	})

	p.SetWorkerNetworkPolicy("wwan0", true, "v4", "")
	if !w.Config.NetworkEnabled || !w.Config.VoWiFiEnabled || w.Config.AirplaneEnabled {
		t.Fatalf("蜂窝开网络应保留软件电话: %+v", w.Config)
	}
	if w.cellularRadioIsSuppressed() {
		t.Fatal("蜂窝开网络不应抑制射频")
	}
}
