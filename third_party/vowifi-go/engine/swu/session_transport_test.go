package swu

import (
	"net"
	"testing"
)

func TestBuildTransportBindsLocalIPWithEphemeralPort(t *testing.T) {
	remote, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer remote.Close()

	session := NewSession(&Config{
		EPDGAddr: remote.LocalAddr().String(),
		LocalIP:  net.ParseIP("127.0.0.1"),
	})
	if err := session.buildTransport(); err != nil {
		t.Fatalf("buildTransport: %v", err)
	}
	defer session.stopTransport()
	if session.socket.LocalPort() == 0 {
		t.Fatal("transport did not bind an ephemeral local port")
	}
}
