package manager

import "testing"

func TestSetDataConfigUpdatesIPFamiliesAndAPN(t *testing.T) {
	m := New(Config{EnableIPv4: true}, nil)

	changed, err := m.SetDataConfig(DataConfig{
		APN:        "ims",
		EnableIPv4: true,
		EnableIPv6: true,
	})
	if err != nil {
		t.Fatalf("SetDataConfig: %v", err)
	}
	if !changed {
		t.Fatal("SetDataConfig changed=false, want true")
	}
	if got := m.dataConfig(); got.APN != "ims" || !got.EnableIPv4 || !got.EnableIPv6 {
		t.Fatalf("dataConfig() = %+v", got)
	}

	changed, err = m.SetDataConfig(m.dataConfig())
	if err != nil || changed {
		t.Fatalf("same SetDataConfig changed=%v err=%v", changed, err)
	}
}

func TestSetDataConfigRejectsNoIPFamily(t *testing.T) {
	m := New(Config{EnableIPv4: true}, nil)
	if _, err := m.SetDataConfig(DataConfig{APN: "ims"}); err == nil {
		t.Fatal("SetDataConfig accepted configuration without an IP family")
	}
	if got := m.dataConfig(); !got.EnableIPv4 || got.EnableIPv6 || got.APN != "" {
		t.Fatalf("invalid update mutated data config: %+v", got)
	}
}
