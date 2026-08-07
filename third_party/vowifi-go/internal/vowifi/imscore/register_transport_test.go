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
		if strings.Contains(request, "@ims.example@") {
			t.Fatalf("REGISTER contains a duplicated identity domain: %q", request)
		}
		if !strings.Contains(request, "From: <sip:310260123456789@ims.example>") ||
			!strings.Contains(request, "Contact: <sip:310260123456789@127.0.0.1:") {
			t.Fatalf("REGISTER identity URIs are invalid: %q", request)
		}
	case <-ctx.Done():
		t.Fatal("registrar did not receive REGISTER")
	}
}

func TestRegistrationRefreshesBeforeExpiryAndReportsFailure(t *testing.T) {
	registrar, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer registrar.Close()
	requests := make(chan string, 2)
	go serveRegistrationSequence(registrar, requests, []int{200, 403})

	svc, err := New(&IMSConfig{
		DeviceID: "dev-refresh", IMSI: "310260123456789", IMPI: "310260123456789@ims.example",
		IMPU: []string{"sip:310260123456789@ims.example"}, Domain: "ims.example",
		LocalIP: net.IPv4(127, 0, 0, 1), Transport: "udp", Registrar: registrar.LocalAddr().String(),
		IMSNetwork: NewSystemIMSNetwork(net.IPv4(127, 0, 0, 1)), AKAProvider: stubAKAProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := svc.Register(ctx); err != nil {
		t.Fatalf("Register: %v", err)
	}

	seen := make([]string, 0, 2)
	for attempt := 0; attempt < 2; attempt++ {
		select {
		case request := <-requests:
			seen = append(seen, request)
		case <-ctx.Done():
			t.Fatalf("received only %d REGISTER requests before timeout", attempt)
		}
	}
	if sipHeaderValue(seen[0], "Call-ID") != sipHeaderValue(seen[1], "Call-ID") {
		t.Fatal("registration refresh changed Call-ID")
	}
	if sipHeaderValue(seen[0], "CSeq") != "1 REGISTER" || sipHeaderValue(seen[1], "CSeq") != "2 REGISTER" {
		t.Fatalf("refresh CSeq values = %q, %q", sipHeaderValue(seen[0], "CSeq"), sipHeaderValue(seen[1], "CSeq"))
	}
	if !strings.Contains(sipHeaderValue(seen[1], "Contact"), ";expires=3600") {
		t.Fatalf("refresh Contact omitted expires: %q", sipHeaderValue(seen[1], "Contact"))
	}
	select {
	case err := <-svc.RegistrationErrors():
		if err == nil || !strings.Contains(err.Error(), "status 403") {
			t.Fatalf("refresh error = %v, want status 403", err)
		}
	case <-ctx.Done():
		t.Fatal("registration refresh failure was not reported")
	}
	if svc.IsRegistered() || svc.RegState() != regFailed {
		t.Fatalf("registration state after refresh failure = %q", svc.RegState())
	}
}

func TestRegistrationExpiresPrefersContactBinding(t *testing.T) {
	response := &sipResponse{Headers: map[string]string{
		"Contact": "<sip:user@10.0.0.2>;expires=120", "Expires": "3600",
	}}
	if got := registrationExpires(response, time.Hour); got != 120*time.Second {
		t.Fatalf("registrationExpires = %s, want 2m", got)
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

func serveRegistrationSequence(conn *net.UDPConn, seen chan<- string, statuses []int) {
	buffer := make([]byte, 64*1024)
	for _, status := range statuses {
		n, remote, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		request := string(buffer[:n])
		seen <- request
		callID := sipHeaderValue(request, "Call-ID")
		response := fmt.Sprintf("SIP/2.0 %d Test\r\nCall-ID: %s\r\nCSeq: 1 REGISTER\r\nExpires: 1\r\nContent-Length: 0\r\n\r\n", status, callID)
		_, _ = conn.WriteToUDP([]byte(response), remote)
	}
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
