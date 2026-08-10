package imscore

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestUSSIProductionProtectedTCPLifecycle(t *testing.T) {
	server, service := newRegisteredTCPUSSIService(t)
	serverResult := make(chan error, 1)
	go func() { serverResult <- serveTCPUSSILifecycle(server) }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	initial, err := service.SendUSSD(ctx, "*100#")
	if err != nil {
		t.Fatal(err)
	}
	if initial.Done || initial.Message != "1. Balance" {
		t.Fatalf("initial result = %+v", initial)
	}
	final, err := service.ContinueUSSD(ctx, initial.SessionID, "1")
	if err != nil {
		t.Fatal(err)
	}
	if !final.Done || final.Message != "Balance: 10" {
		t.Fatalf("continued result = %+v", final)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestUSSIBridgeRejectsRegisteredFlagWithoutLiveSession(t *testing.T) {
	service, err := New(&IMSConfig{
		IMPU: "sip:configured@ims.example", Domain: "ims.example",
		LocalIP: net.IPv4(127, 0, 0, 1), LocalPort: 5060, Transport: "udp",
	})
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.regState = regRegistered
	service.externalTransport = true
	service.mu.Unlock()
	service.regStatus.Store(registrationRegistered)
	t.Cleanup(service.Stop)

	_, err = service.SendUSSD(context.Background(), "*100#")
	if err == nil || !strings.Contains(err.Error(), "registered SIP session is unavailable") {
		t.Fatalf("SendUSSD error = %v", err)
	}
}

func newRegisteredTCPUSSIService(t *testing.T) (net.Conn, *Service) {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	client, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	server, err := listener.AcceptTCP()
	_ = listener.Close()
	if err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	clientPort := client.LocalAddr().(*net.TCPAddr).Port
	service, err := New(&IMSConfig{
		DeviceID: "configured-contact", IMPU: "sip:configured@ims.example",
		Domain: "ims.example", LocalIP: net.IPv4(127, 0, 0, 1),
		LocalPort: clientPort, Transport: "tcp", UserAgent: "ussi-production-test",
	})
	if err != nil {
		_ = client.Close()
		_ = server.Close()
		t.Fatal(err)
	}
	service.mu.Lock()
	service.regState = regRegistered
	service.protectedClientPort = clientPort
	service.protectedServerPort = clientPort
	service.regSession = &registerSession{
		publicID: "sip:registered@ims.example", contactUser: "registered-contact",
		serviceRoute: "<sip:pcscf.ims.example;lr>",
		security: &securityAgreement{
			verifyHeader: "ipsec-3gpp;alg=hmac-sha-1-96",
		},
	}
	service.mu.Unlock()
	service.regStatus.Store(registrationRegistered)
	service.activateProtectedRegistrationTCP(client)
	t.Cleanup(service.Stop)
	t.Cleanup(func() { _ = server.Close() })
	return server, service
}

func serveTCPUSSILifecycle(conn net.Conn) error {
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	invite, err := readTCPUSSIRequest(reader, "INVITE")
	if err != nil {
		return err
	}
	if err := validateRegisteredUSSIHeaders(invite, "tcp"); err != nil {
		return err
	}
	if _, err := io.WriteString(conn, ussiResponseWire(invite, []byte(testUSSIMenuXML), conn.LocalAddr().String())); err != nil {
		return err
	}
	if _, err = readTCPUSSIRequest(reader, "ACK"); err != nil {
		return err
	}
	info, err := readTCPUSSIRequest(reader, "INFO")
	if err != nil {
		return err
	}
	_, err = io.WriteString(conn, ussiResponseWire(info, []byte(testUSSIFinalXML), conn.LocalAddr().String()))
	return err
}

func readTCPUSSIRequest(reader *bufio.Reader, method string) (string, error) {
	request, err := readSIPStreamMessage(reader)
	if err != nil {
		return "", err
	}
	if actual := sipRequestMethod(request); actual != method {
		return "", fmt.Errorf("USSI method = %q, want %q", actual, method)
	}
	return request, nil
}
