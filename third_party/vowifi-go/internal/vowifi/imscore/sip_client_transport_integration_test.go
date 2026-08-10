package imscore

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
)

func TestUDPClientTransactionRetransmitsThroughRegistrationSocket(t *testing.T) {
	registrar, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registrar.Close() })
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(&IMSConfig{LocalIP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	service.transport.timers = sipTransactionTimers{
		t1: 20 * time.Millisecond, t2: 40 * time.Millisecond, bf: 500 * time.Millisecond,
		d: 40 * time.Millisecond, k: 30 * time.Millisecond, m: 40 * time.Millisecond,
	}
	remote := registrar.LocalAddr().(*net.UDPAddr)
	service.mu.Lock()
	service.registrationIO = client
	service.registrationRemote = cloneUDPAddr(remote)
	service.registrationTransport = "udp"
	service.mu.Unlock()
	service.activateInitialSendAndReceive(&initialRegistrationTransport{
		kind: "udp", remote: remote, packet: client, port: client.LocalAddr().(*net.UDPAddr).Port,
	})
	t.Cleanup(service.StopCurrent)

	serverResult := make(chan error, 1)
	go serveTransactionAfterRetransmission(registrar, serverResult)
	request := transactionRequestWithTransport("OPTIONS", "socket-retransmit", "UDP")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := service.transport.RoundTrip(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestFatalClientWriteMarksProductionSignalingDead(t *testing.T) {
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.StopCurrent)
	service.mu.Lock()
	service.regState = regRegistered
	service.signalingReady = true
	service.registrationTransport = "tcp"
	service.externalTransport = true
	service.mu.Unlock()
	service.regStatus.Store(registrationRegistered)
	service.transport.SetSendFn(func(string) error { return syscall.EPIPE })
	request := transactionRequestWithTransport("OPTIONS", "fatal-write", "TCP")
	_, err = service.transport.RoundTrip(context.Background(), request)
	if !errors.Is(err, sip.ErrTransactionTransport) || !errors.Is(err, syscall.EPIPE) {
		t.Fatalf("RoundTrip error = %v", err)
	}
	status := service.StatusCurrent()
	if status.SignalingReady || status.RegStatus != "RejectedTemporary" ||
		!strings.Contains(status.SignalingFailureReason, "broken pipe") {
		t.Fatalf("status after fatal write = %+v", status)
	}
	select {
	case runtimeErr := <-service.RegistrationErrors():
		if !errors.Is(runtimeErr, syscall.EPIPE) {
			t.Fatalf("runtime error = %v", runtimeErr)
		}
	case <-time.After(transactionTestWait):
		t.Fatal("fatal write did not reach runtime error channel")
	}
}

func TestTransactionTimeoutDoesNotMarkSignalingDead(t *testing.T) {
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.StopCurrent)
	service.transport.timers = newTimedTestTransport().timers
	service.mu.Lock()
	service.regState = regRegistered
	service.signalingReady = true
	service.registrationTransport = "tcp"
	service.externalTransport = true
	service.mu.Unlock()
	service.regStatus.Store(registrationRegistered)
	service.transport.SetSendFn(func(string) error { return nil })
	request := transactionRequestWithTransport("OPTIONS", "ordinary-timeout", "TCP")
	_, err = service.transport.RoundTrip(context.Background(), request)
	if !errors.Is(err, sip.ErrTransactionTimeout) {
		t.Fatalf("RoundTrip error = %v", err)
	}
	status := service.StatusCurrent()
	if !status.SignalingReady || status.RegStatus != "Registered" || status.SignalingFailureReason != "" {
		t.Fatalf("status after ordinary timeout = %+v", status)
	}
	select {
	case runtimeErr := <-service.RegistrationErrors():
		t.Fatalf("ordinary timeout reached runtime error channel: %v", runtimeErr)
	default:
	}
}

func TestTCPStreamClosureTerminatesProductionClientTransaction(t *testing.T) {
	client, server := net.Pipe()
	service, err := New(&IMSConfig{LocalIP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.registrationTCP = client
	service.registrationTransport = "tcp"
	service.mu.Unlock()
	service.activateInitialSendAndReceive(&initialRegistrationTransport{
		kind: "tcp", stream: client,
	})
	t.Cleanup(service.StopCurrent)
	serverResult := make(chan error, 1)
	go func() {
		_, readErr := readSIPStreamMessage(bufio.NewReader(server))
		closeErr := server.Close()
		serverResult <- errors.Join(readErr, closeErr)
	}()
	request := transactionRequestWithTransport("OPTIONS", "stream-closed", "TCP")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = service.transport.RoundTrip(ctx, request)
	if !errors.Is(err, sip.ErrTransactionTransport) {
		t.Fatalf("RoundTrip error = %v", err)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func serveTransactionAfterRetransmission(registrar *net.UDPConn, result chan<- error) {
	buffer := make([]byte, 64*1024)
	if err := registrar.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		result <- err
		return
	}
	var first string
	for attempt := 0; attempt < 2; attempt++ {
		n, remote, err := registrar.ReadFromUDP(buffer)
		if err != nil {
			result <- err
			return
		}
		request := string(buffer[:n])
		if attempt == 0 {
			first = request
			continue
		}
		if request != first {
			result <- fmt.Errorf("retransmitted request changed")
			return
		}
		response := transactionResponseWire(request, 200, "OK")
		_, err = registrar.WriteToUDP([]byte(response), remote)
		result <- err
	}
}

func transactionResponseWire(request string, status int, reason string) string {
	return fmt.Sprintf(
		"SIP/2.0 %d %s\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=remote\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
		status, reason, rawSIPHeaderValue(request, "Via"), rawSIPHeaderValue(request, "From"),
		rawSIPHeaderValue(request, "To"), rawSIPHeaderValue(request, "Call-ID"),
		rawSIPHeaderValue(request, "CSeq"),
	)
}
