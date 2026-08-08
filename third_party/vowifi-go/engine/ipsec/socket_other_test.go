//go:build !linux

package ipsec

import (
	"net"
	"strings"
	"testing"
)

func TestNonLinuxKernelSocketFeaturesReturnExplicitErrors(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer conn.Close()
	manager := &SocketManager{Conn: conn}
	if err := manager.SetUDPEncap(); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("SetUDPEncap error = %v", err)
	}
	if _, err := ParseSockExtError(nil); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("ParseSockExtError error = %v", err)
	}
}
