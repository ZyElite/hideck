package imscore

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
)

func TestProtectedRegistrationReconnectsAndReusesAuthorization(t *testing.T) {
	udpServer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udpServer.Close()
	tcpServer, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer tcpServer.Close()
	initialComplete := make(chan struct{})
	serverResult := make(chan error, 1)
	releaseConnection := make(chan struct{})
	defer close(releaseConnection)
	server := protectedRegistrationReconnectServer{
		udp: udpServer, tcp: tcpServer, initialComplete: initialComplete,
		result: serverResult, release: releaseConnection,
	}
	go server.serve()

	svc, err := New(protectedReconnectConfig(udpServer.LocalAddr().String()))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.StopCurrent()
	subscriber := &captureIMSEventSubscriber{events: make(chan events.Event, 1)}
	svc.EventBus().Subscribe(subscriber)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := svc.Register(ctx); err != nil {
		t.Fatalf("initial Register: %v", err)
	}
	assertLocalNumberLearned(t, subscriber.events)
	select {
	case <-initialComplete:
	case <-ctx.Done():
		t.Fatal("initial protected registration did not complete")
	}
	if sent, pingErr := svc.sendPing(); !sent || pingErr == nil {
		t.Fatalf("keepalive during TCP reset = sent %t, err %v", sent, pingErr)
	}
	select {
	case err := <-serverResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("reconnected registration exchange did not complete")
	}
	if !svc.IsRegistered() {
		t.Fatal("automatic protected transport recovery did not restore registration")
	}
	select {
	case runtimeErr := <-svc.RegistrationErrors():
		t.Fatalf("successful in-place recovery requested a full runtime rebuild: %v", runtimeErr)
	default:
	}
}

func assertLocalNumberLearned(t *testing.T, received <-chan events.Event) {
	t.Helper()
	select {
	case event := <-received:
		learned, ok := event.(events.EventLocalNumberLearned)
		if !ok || learned.DevID != "dev-reconnect" || learned.Number != "+447840844894" ||
			learned.Source != "p-associated-uri" || learned.Time.IsZero() {
			t.Fatalf("local number event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("REGISTER did not publish LocalNumberLearned")
	}
}

func protectedReconnectConfig(registrar string) *IMSConfig {
	return &IMSConfig{
		DeviceID: "dev-reconnect", IMEI: "860349055895064", IMSI: "234102356143376",
		IMPI: "234102356143376@ims.example", IMPU: "sip:234102356143376@ims.example",
		Domain: "ims.example", LocalIP: net.IPv4(127, 0, 0, 1), Transport: "udp",
		Registrar: registrar, IMSNetwork: &captureIPSecNetwork{SystemIMSNetwork: NewSystemIMSNetwork(net.IPv4(127, 0, 0, 1))},
		AKAProvider: stubAKAProvider{}, EnableIPSec3GPP: enabledBoolPointer(), Expires: 600000 * time.Second,
	}
}

type protectedRegistrationReconnectServer struct {
	udp             *net.UDPConn
	tcp             *net.TCPListener
	initialComplete chan<- struct{}
	result          chan<- error
	release         <-chan struct{}
}

func (s protectedRegistrationReconnectServer) serve() {
	authorization, err := serveInitialProtectedRegistration(s.udp, s.tcp, s.initialComplete)
	if err != nil {
		s.result <- err
		return
	}
	conn, err := serveReconnectedRegistration(s.tcp, authorization)
	s.result <- err
	if conn != nil {
		<-s.release
		_ = conn.Close()
	}
}

func serveInitialProtectedRegistration(
	udpServer *net.UDPConn,
	tcpServer *net.TCPListener,
	ready chan<- struct{},
) (string, error) {
	buffer := make([]byte, 64*1024)
	n, remote, err := udpServer.ReadFromUDP(buffer)
	if err != nil {
		return "", err
	}
	initial := string(buffer[:n])
	client, err := parseSecurityMechanism(splitSecurityMechanisms(sipHeaderValue(initial, "Security-Client"))[0])
	if err != nil {
		return "", err
	}
	serverHeader := fmt.Sprintf("ipsec-3gpp;q=0.98;alg=hmac-sha-1-96;mod=trans;ealg=aes-cbc;spi-c=858993459;spi-s=1145324612;port-c=6059;port-s=%d", tcpPort(tcpServer.Addr()))
	challenge := strings.TrimPrefix(strings.TrimSpace(digestChallengeHeaderNoQOP()), "WWW-Authenticate: ")
	headers := "WWW-Authenticate: " + challenge + "\r\nSecurity-Server: " + serverHeader + "\r\n"
	if _, err = udpServer.WriteToUDP([]byte(registerWireResponse(initial, 401, headers)), remote); err != nil {
		return "", err
	}
	conn, err := tcpServer.AcceptTCP()
	if err != nil {
		return "", err
	}
	defer func() {
		_ = conn.SetLinger(0)
		_ = conn.Close()
	}()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	registered, err := readSIPStreamMessage(reader)
	if err != nil {
		return "", err
	}
	if tcpPort(conn.RemoteAddr()) != int(client.PortC) {
		return "", fmt.Errorf("protected source port = %d, want %d", tcpPort(conn.RemoteAddr()), client.PortC)
	}
	responseHeaders := "P-Associated-URI: <sip:+447840844894@o2.co.uk>\r\nService-Route: <sip:pcscf.example;lr>\r\n"
	if _, err = conn.Write([]byte(registerWireResponse(registered, 200, responseHeaders))); err != nil {
		return "", err
	}
	subscribe, err := readSIPStreamMessage(reader)
	if err != nil {
		return "", err
	}
	if _, err = conn.Write([]byte(registerWireResponse(subscribe, 200, ""))); err != nil {
		return "", err
	}
	close(ready)
	keepalive, err := readSIPStreamMessage(reader)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(keepalive, "OPTIONS ") {
		return "", fmt.Errorf("request during reset = %q, want OPTIONS", sipRequestMethod(keepalive))
	}
	return sipHeaderValue(registered, "Authorization"), nil
}

func serveReconnectedRegistration(tcpServer *net.TCPListener, authorization string) (net.Conn, error) {
	conn, err := tcpServer.AcceptTCP()
	if err != nil {
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			_ = conn.Close()
		}
	}()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	refresh, err := readSIPStreamMessage(reader)
	if err != nil {
		return nil, err
	}
	if sipHeaderValue(refresh, "CSeq") != "4 REGISTER" {
		return nil, fmt.Errorf("refresh CSeq = %q", sipHeaderValue(refresh, "CSeq"))
	}
	if authorization == "" || strings.Contains(authorization, `response=""`) || sipHeaderValue(refresh, "Authorization") != authorization {
		return nil, errors.New("refresh REGISTER did not reuse the authenticated AKA response")
	}
	if _, err = conn.Write([]byte(registerWireResponse(refresh, 200, ""))); err != nil {
		return nil, err
	}
	subscribe, err := readSIPStreamMessage(reader)
	if err != nil {
		return nil, err
	}
	if sipHeaderValue(subscribe, "Route") != "<sip:pcscf.example;lr>" {
		return nil, errors.New("refresh discarded the established Service-Route")
	}
	if _, err = conn.Write([]byte(registerWireResponse(subscribe, 200, ""))); err != nil {
		return nil, err
	}
	failed = false
	return conn, nil
}
