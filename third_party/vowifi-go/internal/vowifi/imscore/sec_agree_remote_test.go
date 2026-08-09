package imscore

import (
	"context"
	"net"
	"strings"
	"testing"
)

type remoteAddressConn struct {
	net.Conn
	remote net.Addr
}

func (conn remoteAddressConn) RemoteAddr() net.Addr { return conn.remote }

func TestSelectIPSec3GPPRemoteIPPrefersConnectedPeer(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	conn := remoteAddressConn{Conn: left, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 5060}}
	remote, err := selectIPSec3GPPRemoteIP(
		context.Background(), net.ParseIP("192.0.2.20"), conn,
		[]string{"198.51.100.1"}, nil,
	)
	if err != nil || !remote.Equal(net.ParseIP("192.0.2.10")) {
		t.Fatalf("remote = %v, %v", remote, err)
	}
}

func TestSelectIPSec3GPPRemoteIPUsesCandidateOrderAndFamily(t *testing.T) {
	resolver := func(_ context.Context, host string) ([]net.IP, error) {
		switch host {
		case "path.example":
			return []net.IP{net.ParseIP("192.0.2.30")}, nil
		case "registrar.example":
			return []net.IP{net.ParseIP("192.0.2.31"), net.ParseIP("2001:db8::30")}, nil
		default:
			return nil, &net.DNSError{Name: host, Err: "not found"}
		}
	}
	remote, err := selectIPSec3GPPRemoteIP(
		context.Background(), net.ParseIP("2001:db8::20"), nil,
		[]string{"path.example:5060", "registrar.example:5060"}, resolver,
	)
	if err != nil || !remote.Equal(net.ParseIP("2001:db8::30")) {
		t.Fatalf("remote = %v, %v", remote, err)
	}
}

func TestSelectIPSec3GPPRemoteIPReportsFamilyMismatch(t *testing.T) {
	resolver := func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("192.0.2.30")}, nil
	}
	_, err := selectIPSec3GPPRemoteIP(
		context.Background(), net.ParseIP("2001:db8::20"), nil,
		[]string{"registrar.example:5060"}, resolver,
	)
	if err == nil || !strings.Contains(err.Error(), "无同地址族 remote IP: registrar.example") {
		t.Fatalf("error = %v", err)
	}
}

func TestNormalizeRemoteHostCandidate(t *testing.T) {
	tests := map[string]string{
		"sip:user@[2001:db8::1]:5060;lr": "2001:db8::1",
		"SIPS:user@pcscf.example:5061":   "pcscf.example",
		"[2001:db8::2]:5060":             "2001:db8::2",
		"pcscf.example:5060":             "pcscf.example",
	}
	for input, want := range tests {
		if got := normalizeRemoteHostCandidate(input); got != want {
			t.Errorf("normalizeRemoteHostCandidate(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRegisterConnNetworkNormalizesTransportFamily(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	conn := remoteAddressConn{Conn: left, remote: testNetworkAddress{network: "TCP6", value: "[::1]:5060"}}
	if got := registerConnNetwork(conn); got != "tcp" {
		t.Fatalf("registerConnNetwork = %q, want tcp", got)
	}
	if !ipsec3gppTCPNetwork(" TCP4 ") || ipsec3gppTCPNetwork("udp") {
		t.Fatal("ipsec3gppTCPNetwork classification mismatch")
	}
}

type testNetworkAddress struct {
	network string
	value   string
}

func (address testNetworkAddress) Network() string { return address.network }
func (address testNetworkAddress) String() string  { return address.value }
