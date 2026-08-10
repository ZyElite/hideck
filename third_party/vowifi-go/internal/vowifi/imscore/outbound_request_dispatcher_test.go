package imscore

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
)

func TestOutboundDispatcherFreezesSenderAtEnqueue(t *testing.T) {
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Stop)
	oldSent := make(chan string, 2)
	newSent := make(chan string, 1)
	service.transport.SetSendFn(func(raw string) error {
		oldSent <- raw
		return nil
	})

	first := parsedDispatchRequest(t, "frozen-sender", 1)
	second := parsedDispatchRequest(t, "frozen-sender", 2)
	dispatchWithoutWait(t, service, first)
	firstRaw := waitForOutboundSMSControl(t, oldSent)
	dispatchWithoutWait(t, service, second)
	service.transport.SetSendFn(func(raw string) error {
		newSent <- raw
		return nil
	})

	service.transport.DeliverResponse(registerResponseForRequest(firstRaw, 200, nil))
	secondRaw := waitForOutboundSMSControl(t, oldSent)
	if got := rawSIPHeaderValue(secondRaw, "CSeq"); got != "2 MESSAGE" {
		t.Fatalf("second CSeq = %q", got)
	}
	service.transport.DeliverResponse(registerResponseForRequest(secondRaw, 200, nil))
	select {
	case raw := <-newSent:
		t.Fatalf("queued request used replacement sender: %s", rawSIPHeaderValue(raw, "CSeq"))
	case <-time.After(50 * time.Millisecond):
	}
}

func TestOutboundDispatcherFreezesUDPRemoteAtEnqueue(t *testing.T) {
	firstRemote := listenOutboundUDP(t)
	secondRemote := listenOutboundUDP(t)
	client := listenOutboundUDP(t)
	service, err := New(&IMSConfig{LocalIP: net.IPv4(127, 0, 0, 1), Transport: "udp"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Stop)
	service.mu.Lock()
	service.registrationIO = client
	service.registrationRemote = cloneUDPAddr(firstRemote.LocalAddr().(*net.UDPAddr))
	service.registrationTransport = "udp"
	service.mu.Unlock()

	first := parsedDispatchRequest(t, "frozen-remote", 1)
	second := parsedDispatchRequest(t, "frozen-remote", 2)
	dispatchWithoutWait(t, service, first)
	firstRaw := readOutboundUDP(t, firstRemote)
	dispatchWithoutWait(t, service, second)
	service.mu.Lock()
	service.registrationRemote = cloneUDPAddr(secondRemote.LocalAddr().(*net.UDPAddr))
	service.mu.Unlock()

	service.transport.DeliverResponse(registerResponseForRequest(firstRaw, 200, nil))
	secondRaw := readOutboundUDP(t, firstRemote)
	if got := rawSIPHeaderValue(secondRaw, "CSeq"); got != "2 MESSAGE" {
		t.Fatalf("second CSeq = %q", got)
	}
	service.transport.DeliverResponse(registerResponseForRequest(secondRaw, 200, nil))
	assertNoOutboundUDP(t, secondRemote)
}

func dispatchWithoutWait(t *testing.T, service *Service, request *sip.Request) {
	t.Helper()
	if _, _, err := service.dispatchOutboundRequest(context.Background(), "test", request, time.Second, false); err != nil {
		t.Fatal(err)
	}
}

func listenOutboundUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readOutboundUDP(t *testing.T, conn *net.UDPConn) string {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 64*1024)
	n, _, err := conn.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	return string(buffer[:n])
}

func assertNoOutboundUDP(t *testing.T, conn *net.UDPConn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1)
	_, _, err := conn.ReadFromUDP(buffer)
	var networkErr net.Error
	if !errors.As(err, &networkErr) || !networkErr.Timeout() {
		t.Fatalf("replacement endpoint read error = %v, want timeout", err)
	}
}
