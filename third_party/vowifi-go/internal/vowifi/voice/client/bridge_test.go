package client

import (
	"net"
	"testing"
	"time"
)

func TestBridgeRequiresConfiguredTransport(t *testing.T) {
	if err := NewBridge().Start(); err == nil {
		t.Fatal("bridge started without a packet transport")
	}
}

func TestBridgeWritesToConfiguredRemote(t *testing.T) {
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	bridge := NewBridge()
	if err := bridge.ConfigureTransport(TransportConfig{
		Conn: sender, Remote: receiver.LocalAddr(), Contact: "sip:client@127.0.0.1",
		LocalIP: net.IPv4(127, 0, 0, 1),
	}); err != nil {
		t.Fatal(err)
	}
	if err := bridge.Start(); err != nil {
		t.Fatal(err)
	}
	defer bridge.Stop()
	payload := []byte("OPTIONS sip:client SIP/2.0\r\n\r\n")
	if err := bridge.WriteRequest(payload); err != nil {
		t.Fatal(err)
	}
	if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 256)
	n, _, err := receiver.ReadFrom(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if string(buffer[:n]) != string(payload) {
		t.Fatalf("payload = %q", buffer[:n])
	}
	if err := bridge.LastWriteError(); err != nil {
		t.Fatalf("write error = %v", err)
	}
}
