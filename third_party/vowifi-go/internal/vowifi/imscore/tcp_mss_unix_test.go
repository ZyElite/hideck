//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package imscore

import (
	"net"
	"syscall"
	"testing"
)

func TestIPSec3GPPListenerAppliesTCPMSS(t *testing.T) {
	base, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := applyIPSec3GPPPortSListenerTCPMSSWithError(base)
	if err != nil {
		_ = base.Close()
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()
	client, err := net.DialTCP("tcp4", nil, base.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	select {
	case err := <-acceptErr:
		t.Fatal(err)
	case conn := <-accepted:
		defer conn.Close()
		if got := socketTCPMSS(t, conn); got != ipsec3gppTCPMSS {
			t.Fatalf("accepted TCP MSS = %d, want %d", got, ipsec3gppTCPMSS)
		}
	}
}

func socketTCPMSS(t *testing.T, conn net.Conn) int {
	t.Helper()
	syscallConn, ok := conn.(syscall.Conn)
	if !ok {
		t.Fatal("connection does not expose syscall.Conn")
	}
	raw, err := syscallConn.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	value := 0
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		value, socketErr = syscall.GetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_MAXSEG)
	}); err != nil {
		t.Fatal(err)
	}
	if socketErr != nil {
		t.Fatal(socketErr)
	}
	return value
}
