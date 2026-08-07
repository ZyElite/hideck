package imscore

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestRegisterUsesConfiguredIMSNetworkTransport(t *testing.T) {
	registrar, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer registrar.Close()
	requestSeen := make(chan string, 1)
	go serveRegisterStatus(registrar, 200, requestSeen)

	svc, err := New(&IMSConfig{
		DeviceID: "dev-1", IMSI: "310260123456789", IMPI: "310260123456789@ims.example",
		IMPU: []string{"sip:310260123456789@ims.example"}, Domain: "ims.example",
		LocalIP: net.IPv4(127, 0, 0, 1), Transport: "udp", Registrar: registrar.LocalAddr().String(),
		IMSNetwork: NewSystemIMSNetwork(net.IPv4(127, 0, 0, 1)), AKAProvider: stubAKAProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Register(ctx); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !svc.IsRegistered() {
		t.Fatal("service did not enter registered state")
	}
	select {
	case request := <-requestSeen:
		if !strings.HasPrefix(request, "REGISTER sip:ims.example SIP/2.0") {
			t.Fatalf("unexpected request: %q", request)
		}
	case <-ctx.Done():
		t.Fatal("registrar did not receive REGISTER")
	}
}

func TestRegisterPropagatesRegistrarRejection(t *testing.T) {
	registrar, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer registrar.Close()
	go serveRegisterStatus(registrar, 403, nil)
	svc, err := New(&IMSConfig{
		DeviceID: "dev-1", IMSI: "310260123456789", IMPI: "310260123456789@ims.example",
		Domain: "ims.example", LocalIP: net.IPv4(127, 0, 0, 1), Transport: "udp",
		Registrar: registrar.LocalAddr().String(), IMSNetwork: NewSystemIMSNetwork(net.IPv4(127, 0, 0, 1)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Register(ctx); err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("Register error = %v, want 403", err)
	}
	if svc.IsRegistered() || svc.RegState() != regFailed {
		t.Fatalf("registration state = %q", svc.RegState())
	}
}

func serveRegisterStatus(conn *net.UDPConn, status int, seen chan<- string) {
	buffer := make([]byte, 64*1024)
	n, remote, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return
	}
	request := string(buffer[:n])
	if seen != nil {
		seen <- request
	}
	callID := sipHeaderValue(request, "Call-ID")
	response := fmt.Sprintf("SIP/2.0 %d Test\r\nCall-ID: %s\r\nCSeq: 1 REGISTER\r\nContent-Length: 0\r\n\r\n", status, callID)
	_, _ = conn.WriteToUDP([]byte(response), remote)
}

func sipHeaderValue(message, name string) string {
	for _, line := range strings.Split(message, "\r\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
