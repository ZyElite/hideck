package manager

import "testing"

func TestDataInterfacePrefersQMAPMux(t *testing.T) {
	manager := &Manager{cfg: Config{Device: ModemDevice{NetInterface: "wwan0"}}}
	if got := manager.DataInterface(); got != "wwan0" {
		t.Fatalf("DataInterface() = %q, want wwan0", got)
	}
	manager.muxIface = "qmimux3"
	if got := manager.DataInterface(); got != "qmimux3" {
		t.Fatalf("DataInterface() = %q, want qmimux3", got)
	}
}
