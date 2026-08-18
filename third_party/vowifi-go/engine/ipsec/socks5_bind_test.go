package ipsec

import (
	"net"
	"testing"
)

type stubLocalAddrConn struct {
	net.Conn
	local net.Addr
}

func (c stubLocalAddrConn) LocalAddr() net.Addr { return c.local }

func TestSocks5UDPBindAddrFollowsTCPControlChannel(t *testing.T) {
	tcpIP := net.ParseIP("192.0.2.10")
	got := socks5UDPBindAddr(stubLocalAddrConn{local: &net.TCPAddr{IP: tcpIP, Port: 54321}})
	if got == nil || !got.IP.Equal(tcpIP.To4()) || got.Port != 0 {
		t.Fatalf("bind addr = %+v, want 192.0.2.10:0", got)
	}

	unspecified := socks5UDPBindAddr(stubLocalAddrConn{local: &net.TCPAddr{IP: net.IPv4zero, Port: 1}})
	if unspecified == nil || unspecified.IP != nil && !unspecified.IP.IsUnspecified() {
		t.Fatalf("unspecified TCP IP should fall back to wildcard UDP bind, got %+v", unspecified)
	}
}

func TestUDPAssociateAddrUsesBoundUDPSocket(t *testing.T) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer udp.Close()

	got := udpAssociateAddr(udp, stubLocalAddrConn{local: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 9}})
	want := udp.LocalAddr().(*net.UDPAddr)
	if got.Port != want.Port || !got.IP.Equal(want.IP) {
		t.Fatalf("associate addr = %+v, want %+v", got, want)
	}
}
