package imscore

import (
	"context"
	"net"
	"testing"

	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
)

type traceCaptureIMSNetwork struct {
	*SystemIMSNetwork
	traceIDs chan string
}

func (n *traceCaptureIMSNetwork) ResolveIP(ctx context.Context, host string) (net.IP, error) {
	n.traceIDs <- common.TraceID(ctx)
	return net.ParseIP(host), nil
}

func TestSharedRandomHexUsesCharacterLength(t *testing.T) {
	for _, length := range []int{1, 8, 9, 20, 40} {
		if actual := randomHex(length); len(actual) != length {
			t.Fatalf("randomHex(%d) length = %d", length, len(actual))
		}
	}
}

func TestHostOnlyHandlesIPv4AndIPv6(t *testing.T) {
	tests := map[string]string{
		"192.0.2.1:5060":        "192.0.2.1",
		"[2001:db8::1]:5060":    "2001:db8::1",
		"[2001:db8::1]":         "2001:db8::1",
		"2001:db8::1":           "2001:db8::1",
		"registrar.example.com": "registrar.example.com",
	}
	for input, want := range tests {
		if actual := hostOnly(input); actual != want {
			t.Errorf("hostOnly(%q) = %q, want %q", input, actual, want)
		}
	}
}

func TestSystemIMSNetworkFindsHostAddress(t *testing.T) {
	network := NewSystemIMSNetwork(net.IPv4(192, 0, 2, 1))
	if !network.HasLocalIP(net.IPv4(127, 0, 0, 1)) {
		t.Fatal("system IMS network did not find loopback")
	}
	if network.HasLocalIP(net.IPv4(203, 0, 113, 1)) {
		t.Fatal("system IMS network found an unassigned TEST-NET address")
	}
}

func TestRegisterPropagatesGeneratedTraceID(t *testing.T) {
	registrar, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer registrar.Close()
	go serveRegisterStatus(registrar, 200, nil)

	network := &traceCaptureIMSNetwork{
		SystemIMSNetwork: NewSystemIMSNetwork(net.IPv4(127, 0, 0, 1)),
		traceIDs:         make(chan string, 1),
	}
	config := registerTransportTestConfig("udp", registrar.LocalAddr().String())
	config.IMSNetwork = network
	service, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Stop()
	if err := service.Register(nil); err != nil {
		t.Fatal(err)
	}
	if traceID := <-network.traceIDs; len(traceID) != 16 {
		t.Fatalf("REGISTER trace ID = %q", traceID)
	}
}
