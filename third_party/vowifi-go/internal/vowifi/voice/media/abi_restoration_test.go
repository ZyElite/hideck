package media

import (
	"net"
	"reflect"
	"testing"
	"time"
)

type originalRTPRelayLifecycle interface {
	Start()
	Stop()
	StartPCAP(string) error
	StopPCAP()
}

var _ originalRTPRelayLifecycle = (*RTPRelay)(nil)

func TestOriginalMediaStructPrefixes(t *testing.T) {
	assertFieldPrefix(t, reflect.TypeOf(RTPRelay{}), []string{
		"bytesIMSToLAN", "bytesLANToIMS", "bytesIMSRTCPToLAN", "bytesLANRTCPToIMS",
		"connIMS", "connLAN", "connIMSRTCP", "connLANRTCP", "remoteAddr", "remoteAddrRTCP",
		"clientAddr", "clientAddrRTCP", "stopCh", "stopOnce", "mu", "active",
		"imsFirstPacket", "lanFirstPacket", "imsRTCPFirstPacket", "lanRTCPFirstPacket",
		"Monitor", "rtcpKeepaliveTimer", "rtcpMu", "ptMap", "deviceID", "traceID",
		"pcapFile", "pcapMu", "pcapEnable",
	})
	assertFieldPrefix(t, reflect.TypeOf(ComfortNoiseGenerator{}), []string{
		"conn", "remoteAddr", "seqNum", "timestamp", "ssrc", "seed", "stopCh",
		"stopOnce", "wg", "deviceID", "traceID",
	})
	assertFieldPrefix(t, reflect.TypeOf(MediaSessionManager{}), []string{
		"mu", "relay", "deviceID", "traceID", "EventCh", "released",
	})
	assertFieldPrefix(t, reflect.TypeOf(Bridge{}), []string{"deviceID", "endpoint"})
}

func assertFieldPrefix(t *testing.T, value reflect.Type, fields []string) {
	t.Helper()
	for index, field := range fields {
		if index >= value.NumField() || value.Field(index).Name != field {
			t.Fatalf("%s field %d = %q, want %q", value.Name(), index, value.Field(index).Name, field)
		}
	}
}

func TestOriginalMediaCallForms(t *testing.T) {
	relay, err := NewRTPRelayWithListener(nil, "127.0.0.1", "127.0.0.1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relay.Stop)
	relay.SetLogContext("device-30", "trace-30")
	relay.SetPTMapping(96, 8)
	if err := relay.SetRemoteAddr("127.0.0.1", 20000); err != nil {
		t.Fatal(err)
	}
	if err := relay.SetClientAddr("127.0.0.1", 21000); err != nil {
		t.Fatal(err)
	}
	relay.EnableMonitor(int64(time.Second), func() {})
	relay.SetOneWayTimeoutHandler(func(string) {})
	if relay.IMSPort() == 0 || relay.LANPort() == 0 {
		t.Fatal("original constructor did not bind RTP ports")
	}

	manager := NewMediaSessionManager("device-30", "trace-30")
	managed, err := manager.CreateRelay("127.0.0.1", "127.0.0.1", int64(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if manager.GetRelay() != managed {
		t.Fatal("original manager relay was not retained")
	}
	if err := manager.Release(); err != nil {
		t.Fatal(err)
	}

	conn := listenMediaUDP(t)
	noise := NewComfortNoiseGenerator(conn, conn.LocalAddr().(*net.UDPAddr), "device-30", "trace-30")
	if err := noise.Start(); err != nil {
		t.Fatal(err)
	}
	noise.Stop()
}

func TestCurrentRTPRelayLifecycleErrorsRemainExplicit(t *testing.T) {
	var nilRelay *RTPRelay
	if err := nilRelay.StartCurrent(); err == nil {
		t.Fatal("nil relay start did not return an error")
	}
	nilRelay.Start()
	nilRelay.Stop()

	relay := NewRTPRelay(nil, nil)
	relay.Stop()
	if err := relay.StartCurrent(); err == nil {
		t.Fatal("stopped relay restart did not return an error")
	}
	relay.Start()
}
