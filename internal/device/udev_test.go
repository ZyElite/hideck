//go:build linux

package device

import "testing"

func TestUdevWatcherTreatsWWANPortEventsAsModemEvents(t *testing.T) {
	w := NewUdevWatcher(nil)
	event := []byte("add@/devices/platform/soc@0/4080000.remoteproc/wwan/wwan0/wwan0qmi0\x00ACTION=add\x00SUBSYSTEM=wwan\x00DEVTYPE=wwan_port\x00DEVNAME=/dev/wwan0qmi0\x00")

	if !w.isModemEvent(event) {
		t.Fatal("isModemEvent() = false, want true for SUBSYSTEM=wwan QMI port")
	}
}

func TestUdevWatcherKeepsIgnoringNonWWANNetEvents(t *testing.T) {
	w := NewUdevWatcher(nil)
	event := []byte("add@/devices/virtual/net/eth0\x00ACTION=add\x00SUBSYSTEM=net\x00INTERFACE=eth0\x00")

	if w.isModemEvent(event) {
		t.Fatal("isModemEvent() = true, want false for eth0 net event")
	}
}

func TestUdevKernelUeventGroupIsMulticastGroupOne(t *testing.T) {
	if udevKernelUeventGroup != 1 {
		t.Fatalf("udevKernelUeventGroup = %d, want 1 so USB add/remove broadcasts are received", udevKernelUeventGroup)
	}
}

func TestUdevWatcherTreatsCDCWdmRemoveAsModemEvent(t *testing.T) {
	w := NewUdevWatcher(nil)
	event := []byte("remove@/devices/pci0000:00/usb2/2-1/2-1:1.4/usbmisc/cdc-wdm0\x00ACTION=remove\x00SUBSYSTEM=usbmisc\x00DEVNAME=/dev/cdc-wdm0\x00")
	if !w.isModemEvent(event) {
		t.Fatal("isModemEvent() = false, want true for cdc-wdm remove")
	}
}
