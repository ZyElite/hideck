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
	go serveProtectedRegistrationReconnect(udpServer, tcpServer, initialComplete, serverResult)

	svc, err := New(protectedReconnectConfig(udpServer.LocalAddr().String()))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := svc.Register(ctx); err != nil {
		t.Fatalf("initial Register: %v", err)
	}
	select {
	case <-initialComplete:
	case <-ctx.Done():
		t.Fatal("initial protected registration did not complete")
	}
	waitForRegistrationTCPClose(t, svc)
	if err := svc.TriggerRegisterImmediate(); err != nil {
		t.Fatalf("refresh Register: %v", err)
	}
	select {
	case err := <-serverResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("reconnected registration exchange did not complete")
	}
}

func protectedReconnectConfig(registrar string) *IMSConfig {
	return &IMSConfig{
		DeviceID: "dev-reconnect", IMEI: "860349055895064", IMSI: "234102356143376",
		IMPI: "234102356143376@ims.example", IMPU: []string{"sip:234102356143376@ims.example"},
		Domain: "ims.example", LocalIP: net.IPv4(127, 0, 0, 1), Transport: "udp",
		Registrar: registrar, IMSNetwork: &captureIPSecNetwork{SystemIMSNetwork: NewSystemIMSNetwork(net.IPv4(127, 0, 0, 1))},
		AKAProvider: stubAKAProvider{}, IPSec3GPPEnabled: true, Expires: 600000 * time.Second,
	}
}

func waitForRegistrationTCPClose(t *testing.T, svc *Service) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		svc.mu.RLock()
		closed := svc.registrationTCP == nil
		svc.mu.RUnlock()
		if closed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("closed protected TCP connection remained active")
}

func serveProtectedRegistrationReconnect(udpServer *net.UDPConn, tcpServer *net.TCPListener, initialComplete chan<- struct{}, result chan<- error) {
	authorization, err := serveInitialProtectedRegistration(udpServer, tcpServer)
	if err != nil {
		result <- err
		return
	}
	close(initialComplete)
	result <- serveReconnectedRegistration(tcpServer, authorization)
}

func serveInitialProtectedRegistration(udpServer *net.UDPConn, tcpServer *net.TCPListener) (string, error) {
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
	return sipHeaderValue(registered, "Authorization"), nil
}

func serveReconnectedRegistration(tcpServer *net.TCPListener, authorization string) error {
	conn, err := tcpServer.AcceptTCP()
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	refresh, err := readSIPStreamMessage(reader)
	if err != nil {
		return err
	}
	if sipHeaderValue(refresh, "CSeq") != "4 REGISTER" {
		return fmt.Errorf("refresh CSeq = %q", sipHeaderValue(refresh, "CSeq"))
	}
	if authorization == "" || strings.Contains(authorization, `response=""`) || sipHeaderValue(refresh, "Authorization") != authorization {
		return errors.New("refresh REGISTER did not reuse the authenticated AKA response")
	}
	if _, err = conn.Write([]byte(registerWireResponse(refresh, 200, ""))); err != nil {
		return err
	}
	subscribe, err := readSIPStreamMessage(reader)
	if err != nil {
		return err
	}
	if sipHeaderValue(subscribe, "Route") != "<sip:pcscf.example;lr>" {
		return errors.New("refresh discarded the established Service-Route")
	}
	_, err = conn.Write([]byte(registerWireResponse(subscribe, 200, "")))
	return err
}
