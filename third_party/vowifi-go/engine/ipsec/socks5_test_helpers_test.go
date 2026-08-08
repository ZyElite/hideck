package ipsec

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

type testSocks5Proxy struct {
	t      *testing.T
	tcp    net.Listener
	udp    *net.UDPConn
	done   chan struct{}
	errors chan error
}

func newTestSocks5Proxy(t *testing.T) *testSocks5Proxy {
	t.Helper()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen TCP: %v", err)
	}
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		_ = tcp.Close()
		t.Fatalf("listen UDP: %v", err)
	}
	proxy := &testSocks5Proxy{
		t: t, tcp: tcp, udp: udp, done: make(chan struct{}), errors: make(chan error, 1),
	}
	go proxy.serveControl()
	return proxy
}

func (p *testSocks5Proxy) address() string { return p.tcp.Addr().String() }

func (p *testSocks5Proxy) serveControl() {
	conn, err := p.tcp.Accept()
	if err != nil {
		p.report(err)
		return
	}
	defer conn.Close()
	if err := acceptNoAuthHandshake(conn); err != nil {
		p.report(err)
		return
	}
	if err := acceptUDPAssociate(conn, p.udp.LocalAddr().(*net.UDPAddr)); err != nil {
		p.report(err)
		return
	}
	<-p.done
}

func acceptNoAuthHandshake(conn net.Conn) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	_, err := conn.Write([]byte{socks5Version, socks5MethodNoAuth})
	return err
}

func acceptUDPAssociate(conn net.Conn, relay *net.UDPAddr) error {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	if header[0] != socks5Version || header[1] != socks5CmdUDPAssociate {
		return errors.New("unexpected SOCKS5 UDP associate request")
	}
	addressLength := map[byte]int{socks5ATYPIPv4: 4, socks5ATYPIPv6: 16}[header[3]]
	if addressLength == 0 {
		return errors.New("unsupported test SOCKS5 address type")
	}
	if _, err := io.ReadFull(conn, make([]byte, addressLength+2)); err != nil {
		return err
	}
	reply := []byte{socks5Version, 0, 0, socks5ATYPIPv4, 0, 0, 0, 0}
	reply = binary.BigEndian.AppendUint16(reply, uint16(relay.Port))
	_, err := conn.Write(reply)
	return err
}

func (p *testSocks5Proxy) receive(t *testing.T) ([]byte, *net.UDPAddr) {
	t.Helper()
	_ = p.udp.SetReadDeadline(time.Now().Add(transportTestTimeout))
	buffer := make([]byte, 65535)
	length, source, err := p.udp.ReadFromUDP(buffer)
	if err != nil {
		t.Fatalf("proxy receive: %v", err)
	}
	return append([]byte(nil), buffer[:length]...), source
}

func (p *testSocks5Proxy) send(t *testing.T, target *net.UDPAddr, data []byte) {
	t.Helper()
	if _, err := p.udp.WriteToUDP(data, target); err != nil {
		t.Fatalf("proxy send: %v", err)
	}
}

func (p *testSocks5Proxy) report(err error) {
	select {
	case p.errors <- err:
	default:
	}
}

func (p *testSocks5Proxy) close() {
	close(p.done)
	_ = p.tcp.Close()
	_ = p.udp.Close()
	select {
	case err := <-p.errors:
		p.t.Errorf("SOCKS5 proxy: %v", err)
	default:
	}
}
