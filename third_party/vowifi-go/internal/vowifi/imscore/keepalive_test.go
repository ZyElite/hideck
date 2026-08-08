package imscore

import (
	"bufio"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestProtectedSIPStreamClosureInvalidatesRegistration(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	client, server := net.Pipe()
	defer server.Close()
	service.activateProtectedRegistrationTCP(client)
	if readiness := service.SMSReadiness(); !readiness.Ready {
		t.Fatalf("initial readiness = %+v", readiness)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}

	waitForRegistrationState(t, service, regFailed)
	readiness := service.SMSReadiness()
	if readiness.Ready || readiness.TransportReady {
		t.Fatalf("readiness after close = %+v", readiness)
	}
	select {
	case err := <-service.RegistrationErrors():
		if !strings.Contains(err.Error(), "registration SIP stream closed") {
			t.Fatalf("registration error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("registration stream closure was not reported")
	}
}

func TestPreviousSIPStreamClosureDoesNotInvalidateReplacement(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	oldClient, oldServer := net.Pipe()
	service.activateProtectedRegistrationTCP(oldClient)
	newClient, newServer := net.Pipe()
	service.activateProtectedRegistrationTCP(newClient)
	t.Cleanup(func() {
		_ = oldServer.Close()
		_ = newServer.Close()
	})

	if err := oldServer.Close(); err != nil {
		t.Fatal(err)
	}
	request := "OPTIONS sip:test@example SIP/2.0\r\nCall-ID: replacement\r\nCSeq: 1 OPTIONS\r\nContent-Length: 0\r\n\r\n"
	done := make(chan error, 1)
	go func() {
		received, err := readSIPStreamMessage(bufio.NewReader(newServer))
		if err == nil && received != request {
			err = errors.New("replacement stream received unexpected request")
		}
		done <- err
	}()
	if err := service.transport.Send(request); err != nil {
		t.Fatalf("send on replacement stream: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if service.RegState() != regRegistered || !service.SMSReadiness().TransportReady {
		t.Fatalf("replacement registration was invalidated: %+v", service.SMSReadiness())
	}
}

func TestIMSKeepaliveUsesRegisteredProtectedProfile(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	client, server := net.Pipe()
	defer server.Close()
	service.activateProtectedRegistrationTCP(client)
	done := make(chan error, 1)
	go func() {
		request, err := readSIPStreamMessage(bufio.NewReader(server))
		if err != nil {
			done <- err
			return
		}
		if !strings.HasPrefix(request, "OPTIONS sip:+447840844894@o2.co.uk SIP/2.0") {
			done <- errors.New("keepalive did not target the registered public identity")
			return
		}
		if rawSIPHeaderValue(request, "Security-Verify") == "" ||
			rawSIPHeaderValue(request, "Route") == "" {
			done <- errors.New("keepalive omitted registered security routing")
			return
		}
		_, err = io.WriteString(server, registerWireResponse(request, 200, ""))
		done <- err
	}()
	if err := service.sendIMSKeepalive(); err != nil {
		t.Fatalf("sendIMSKeepalive: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func newProtectedKeepaliveTestService(t *testing.T) *Service {
	t.Helper()
	service, err := New(&IMSConfig{
		DeviceID: "wwan0", Domain: "ims.example", SMSC: "+447802002606",
		LocalIP: net.IPv4(10, 0, 0, 2), Transport: "tcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.regState = regRegistered
	service.smsReceiverReady = true
	service.protectedClientPort = 16082
	service.protectedServerPort = 16083
	service.regSession = &registerSession{
		contactUser: "registered-contact", cseq: 3,
		publicID: "sip:+447840844894@o2.co.uk", serviceRoute: "<sip:pcscf.ims.example;lr>",
		security: &securityAgreement{verifyHeader: "ipsec-3gpp;alg=hmac-sha-1-96"},
	}
	service.mu.Unlock()
	t.Cleanup(service.Stop)
	return service
}

func waitForRegistrationState(t *testing.T, service *Service, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if service.RegState() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("registration state = %s, want %s", service.RegState(), want)
}
