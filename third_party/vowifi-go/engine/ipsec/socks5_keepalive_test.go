package ipsec

import (
	"testing"
	"time"
)

func TestSocks5UDPKeepaliveSendsWhenIdle(t *testing.T) {
	previous := socks5UDPKeepaliveEvery
	socks5UDPKeepaliveEvery = 40 * time.Millisecond
	t.Cleanup(func() { socks5UDPKeepaliveEvery = previous })

	proxy := newTestSocks5Proxy(t)
	defer proxy.close()
	transport, err := NewSocks5Transport(Socks5Config{
		ProxyAddr: proxy.address(), RemoteAddr: "127.0.0.1:500", DeviceID: "keepalive",
	})
	if err != nil {
		t.Fatalf("NewSocks5Transport: %v", err)
	}
	if err := transport.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer transport.Stop()

	wire, _ := proxy.receive(t)
	datagram, err := DecodeSocks5UDPDatagram(wire)
	if err != nil {
		t.Fatalf("decode keepalive: %v", err)
	}
	if len(datagram.Data) != 1 || datagram.Data[0] != 0xff {
		t.Fatalf("keepalive payload = %x", datagram.Data)
	}
}
