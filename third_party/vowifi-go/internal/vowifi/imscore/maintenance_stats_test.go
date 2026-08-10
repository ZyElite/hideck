package imscore

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestInboundCountingPacketConnProjectsSocketStats(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	reader, writer := packetConnPair(t)
	counted := &inboundCountingPacketConn{conn: reader, service: service}
	payload := []byte("SIP/2.0 200 OK\r\nContent-Length: 0\r\n\r\n")
	go func() { _, _ = writer.WriteTo(payload, reader.LocalAddr()) }()

	buffer := make([]byte, 256)
	n, _, err := counted.ReadFrom(buffer)
	if err != nil {
		t.Fatal(err)
	}
	stats := service.captureInboundStats()
	if stats.UDPSocketReads != 1 || stats.UDPSocketBytes != uint64(n) {
		t.Fatalf("UDP stats = reads %d bytes %d, want 1/%d", stats.UDPSocketReads, stats.UDPSocketBytes, n)
	}
}

func TestInboundCountingConnRecordsTrafficBeforeSIPParsing(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	service.pingFailCount.Store(2)
	service.lastPingOK.Store(false)
	client, server := net.Pipe()
	counted := service.newInboundCountingConn(client)
	payload := []byte("partial")
	go func() { _, _ = server.Write(payload) }()

	buffer := make([]byte, len(payload))
	if _, err := io.ReadFull(counted, buffer); err != nil {
		t.Fatal(err)
	}
	stats := service.captureInboundStats()
	if stats.TCPSocketReads != 1 || stats.TCPSocketBytes != uint64(len(payload)) {
		t.Fatalf("TCP stats = reads %d bytes %d", stats.TCPSocketReads, stats.TCPSocketBytes)
	}
	status := service.StatusCurrent()
	if status.PingFailCount != 0 || !status.LastPingOK {
		t.Fatalf("ping status = count %d ok %v", status.PingFailCount, status.LastPingOK)
	}
}

func TestDispatchInboundSIPProjectsParsedMessageStats(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	response := "SIP/2.0 200 OK\r\nCall-ID: response\r\nCSeq: 1 OPTIONS\r\nContent-Length: 0\r\n\r\n"
	if err := service.dispatchInboundSIP(response, nil); err != nil {
		t.Fatal(err)
	}
	request := "OPTIONS sip:user@example.test SIP/2.0\r\nVia: SIP/2.0/UDP host;branch=z9hG4bK-a\r\nFrom: <sip:a@example.test>;tag=a\r\nTo: <sip:b@example.test>\r\nCall-ID: request\r\nCSeq: 1 OPTIONS\r\nContent-Length: 0\r\n\r\n"
	if err := service.dispatchInboundSIP(request, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	stats := service.captureInboundStats()
	if stats.SIPParsedMessages != 2 || stats.SIPParsedRequests != 1 || stats.SIPParsedResponses != 1 {
		t.Fatalf("parsed stats = %+v", stats)
	}
	if got := service.StatusCurrent().Diagnostics["inbound_sip_parsed_messages"]; got != uint64(2) {
		t.Fatalf("status parsed messages = %#v, want 2", got)
	}
}

func TestSendPingAllowsOnlyOneInFlightTransaction(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	client, server := net.Pipe()
	service.activateProtectedRegistrationTCP(client)
	received := make(chan string, 1)
	release := make(chan struct{})
	go holdKeepaliveResponse(server, received, release)

	firstDone := make(chan error, 1)
	go func() {
		_, err := service.sendPing()
		firstDone <- err
	}()
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("first ping was not sent")
	}
	if sent, err := service.sendPing(); sent || err != nil {
		t.Fatalf("second ping = sent %v err %v, want skipped", sent, err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if reads := service.captureInboundStats().TCPSocketReads; reads == 0 {
		t.Fatal("protected registration TCP response bypassed the production counting wrapper")
	}
}

func holdKeepaliveResponse(conn net.Conn, received chan<- string, release <-chan struct{}) {
	request, err := readSIPStreamMessage(bufio.NewReader(conn))
	if err != nil {
		return
	}
	received <- request
	<-release
	_, _ = io.WriteString(conn, registerWireResponse(request, 200, ""))
}

func TestInboundStatsLoggerStopsWithReceiverLifecycle(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	service.startInboundStatsLogger(context.Background(), inboundStatsLoggerOptions{Interval: time.Millisecond})
	service.inboundStatsMu.Lock()
	done := service.inboundStatsDone
	service.inboundStatsMu.Unlock()
	service.stopInboundStatsLogger()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("inbound stats logger did not stop")
	}
}

func TestServiceStopCancelsInboundStatsLogger(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	service.startInboundStatsLogger(context.Background(), inboundStatsLoggerOptions{Interval: time.Hour})
	service.inboundStatsMu.Lock()
	done := service.inboundStatsDone
	service.inboundStatsMu.Unlock()
	service.StopCurrent()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Service.Stop did not join the inbound stats logger")
	}
}

func TestRegistrationPacketReadFailureMarksSignalingDead(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	packet, peer := packetConnPair(t)
	service.mu.Lock()
	service.signalingReady = true
	service.registrationIO = packet
	service.registrationRefreshAt = time.Now().Add(time.Hour)
	service.mu.Unlock()
	service.handleRegistrationPacketReadError(packet, errors.New("read failed"))
	status := service.StatusCurrent()
	if status.SignalingReady || status.RegState != regFailed {
		t.Fatalf("status after packet read failure = ready %v state %s", status.SignalingReady, status.RegState)
	}
	if status.SignalingFailureReason == "" {
		t.Fatal("packet read failure reason was not projected")
	}
	_ = peer
}

func TestStaleRegistrationPacketReadFailureCannotDetachReplacement(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	stale, _ := packetConnPair(t)
	current, _ := packetConnPair(t)
	service.mu.Lock()
	service.registrationIO = current
	service.signalingReady = true
	service.mu.Unlock()
	service.handleRegistrationPacketReadError(stale, errors.New("stale socket closed"))
	service.mu.RLock()
	got := service.registrationIO
	ready := service.signalingReady
	service.mu.RUnlock()
	if got != current || !ready {
		t.Fatalf("stale read detached current packet: current=%v ready=%v", got == current, ready)
	}
}

func TestMORPErrorCause30SchedulesRateLimitedRegistration(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	service.mu.Lock()
	service.registrationRefreshAt = time.Now().Add(time.Hour)
	service.mu.Unlock()

	service.handleMORPError(30, "SM absent")
	service.mu.RLock()
	firstDeadline := service.registrationRefreshAt
	service.mu.RUnlock()
	if firstDeadline.After(time.Now()) || !service.reRegisterPending.Load() {
		t.Fatalf("cause 30 deadline = %s pending = %v", firstDeadline, service.reRegisterPending.Load())
	}
	if action := service.nextIMSMaintenanceAction(time.Now()); action != imsMaintenanceRefresh {
		t.Fatalf("maintenance action = %d, want refresh", action)
	}

	service.mu.Lock()
	service.registrationRefreshAt = time.Now().Add(time.Hour)
	service.mu.Unlock()
	service.handleMORPError(30, "duplicate")
	service.mu.RLock()
	secondDeadline := service.registrationRefreshAt
	service.mu.RUnlock()
	if !secondDeadline.After(time.Now().Add(59 * time.Minute)) {
		t.Fatalf("rate-limited cause 30 changed deadline to %s", secondDeadline)
	}
	if got := service.moRPErrorCause30.Load(); got != 2 {
		t.Fatalf("cause 30 count = %d, want 2", got)
	}
}

func TestMORPErrorCause30WakesProductionRegistrationLoop(t *testing.T) {
	registrar, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer registrar.Close()
	seen := make(chan string, 2)
	go serveMaintenanceRegistrationSequence(registrar, seen, 2)
	service, err := New(registerTransportTestConfig("udp", registrar.LocalAddr().String()))
	if err != nil {
		t.Fatal(err)
	}
	defer service.StopCurrent()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.Register(ctx); err != nil {
		t.Fatal(err)
	}
	<-seen
	service.handleMORPError(30, "SM absent")
	select {
	case request := <-seen:
		if !strings.HasPrefix(request, "REGISTER ") {
			t.Fatalf("maintenance request = %q", request)
		}
	case <-time.After(time.Second):
		t.Fatal("cause 30 did not wake the production REGISTER loop")
	}
}

func serveMaintenanceRegistrationSequence(conn *net.UDPConn, seen chan<- string, count int) {
	buffer := make([]byte, 64*1024)
	for attempt := 0; attempt < count; attempt++ {
		n, remote, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		request := string(buffer[:n])
		seen <- request
		response := registerWireResponse(request, 200, "Expires: 3600\r\n")
		_, _ = conn.WriteToUDP([]byte(response), remote)
	}
}

func packetConnPair(t *testing.T) (net.PacketConn, net.PacketConn) {
	t.Helper()
	reader, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	writer, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		reader.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close(); writer.Close() })
	return reader, writer
}
