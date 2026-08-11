package mbimcore

import "testing"

func TestDataInterfaceReturnsConfiguredMBIMInterface(t *testing.T) {
	manager := &Manager{dataCfg: DataConfig{Interface: "wwan1"}}
	if got := manager.DataInterface(); got != "wwan1" {
		t.Fatalf("DataInterface() = %q, want wwan1", got)
	}
}
