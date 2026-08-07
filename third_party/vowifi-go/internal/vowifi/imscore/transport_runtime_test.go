package imscore

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestReceiverRuntimeRespondsToUDPOptionsAndStops(t *testing.T) {
	registrar, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer registrar.Close()
	response := make(chan string, 1)
	go serveRegisterAndOptions(registrar, response)

	service, err := New(&IMSConfig{
		DeviceID: "dev-udp", IMSI: "234100000000001", IMPI: "234100000000001@ims.example",
		IMPU: []string{"sip:234100000000001@ims.example"}, Domain: "ims.example", SMSC: "+123",
		LocalIP: net.IPv4(127, 0, 0, 1), Registrar: registrar.LocalAddr().String(),
		IMSNetwork: NewSystemIMSNetwork(net.IPv4(127, 0, 0, 1)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.Register(ctx); err != nil {
		t.Fatalf("Register: %v", err)
	}
	select {
	case got := <-response:
		if !strings.HasPrefix(got, "SIP/2.0 200 OK") || rawSIPHeaderValue(got, "CSeq") != "1 OPTIONS" {
			t.Fatalf("OPTIONS response = %q", got)
		}
	case <-ctx.Done():
		t.Fatal("did not receive OPTIONS response")
	}
	if status := service.SMSReceiverTransport().(SMSReceiverStatus); !status.Active {
		t.Fatalf("receiver status = %+v", status)
	}
	service.Stop()
	if status := service.SMSReceiverTransport().(SMSReceiverStatus); status.Active {
		t.Fatalf("receiver remained active after Stop: %+v", status)
	}
}

func TestReceiverRuntimeServesConcurrentTCPConnections(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}
	service, err := New(&IMSConfig{LocalIP: net.IPv4(127, 0, 0, 1), SMSC: "+123"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	service.securityServerIO = listener
	service.networkDone.Add(1)
	go service.acceptProtectedSIP(listener)
	waitForReceiverActive(t, service)

	first := dialReceiverClient(t, listener.Addr())
	defer first.Close()
	second := dialReceiverClient(t, listener.Addr())
	defer second.Close()
	assertOptionsRoundTrip(t, first, "first-call")
	assertOptionsRoundTrip(t, second, "second-call")
	service.Stop()
}

func serveRegisterAndOptions(conn *net.UDPConn, response chan<- string) {
	buffer := make([]byte, 64*1024)
	n, remote, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return
	}
	request := string(buffer[:n])
	_, _ = conn.WriteToUDP([]byte(registerWireResponse(request, 200, "")), remote)
	_, _ = conn.WriteToUDP([]byte(optionsRequest("udp-options")), remote)
	n, _, err = conn.ReadFromUDP(buffer)
	if err == nil {
		response <- string(buffer[:n])
	}
}

func waitForReceiverActive(t *testing.T, service *Service) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if service.receiverStatus().Active {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("receiver did not become active")
}

func dialReceiverClient(t *testing.T, address net.Addr) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address.String(), time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return conn
}

func assertOptionsRoundTrip(t *testing.T, conn net.Conn, callID string) {
	t.Helper()
	if _, err := fmt.Fprint(conn, optionsRequest(callID)); err != nil {
		t.Fatalf("write OPTIONS: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	response, err := readSIPStreamMessage(bufio.NewReader(conn))
	if err != nil {
		t.Fatalf("read OPTIONS response: %v", err)
	}
	if !strings.HasPrefix(response, "SIP/2.0 200 OK") || rawSIPHeaderValue(response, "Call-ID") != callID {
		t.Fatalf("OPTIONS response = %q", response)
	}
}

func optionsRequest(callID string) string {
	return "OPTIONS sip:user@ims.example SIP/2.0\r\n" +
		"Via: SIP/2.0/TCP 127.0.0.1:5060;branch=z9hG4bK-options\r\n" +
		"From: <sip:server@ims.example>;tag=server\r\n" +
		"To: <sip:user@ims.example>\r\nCall-ID: " + callID + "\r\n" +
		"CSeq: 1 OPTIONS\r\nContent-Length: 0\r\n\r\n"
}
