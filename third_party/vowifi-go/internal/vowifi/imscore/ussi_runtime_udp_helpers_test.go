package imscore

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func newRegisteredUDPUSSIService(t *testing.T) (*net.UDPConn, *Service) {
	t.Helper()
	registrar, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		_ = registrar.Close()
		t.Fatal(err)
	}
	clientPort := client.LocalAddr().(*net.UDPAddr).Port
	service, err := New(&IMSConfig{
		DeviceID: "configured-contact", IMPU: "sip:configured@ims.example",
		Domain: "ims.example", LocalIP: net.IPv4(127, 0, 0, 1),
		LocalPort: clientPort, Transport: "udp", UserAgent: "ussi-production-test",
	})
	if err != nil {
		_ = client.Close()
		_ = registrar.Close()
		t.Fatal(err)
	}
	remote := registrar.LocalAddr().(*net.UDPAddr)
	service.mu.Lock()
	service.regState = regRegistered
	service.registrationIO = client
	service.registrationRemote = cloneUDPAddr(remote)
	service.registrationTransport = "udp"
	service.regSession = &registerSession{
		publicID: "sip:registered@ims.example", contactUser: "registered-contact",
		serviceRoute: "<sip:pcscf.ims.example;lr>",
	}
	service.mu.Unlock()
	service.regStatus.Store(registrationRegistered)
	service.activateInitialSendAndReceive(&initialRegistrationTransport{
		kind: "udp", remote: remote, packet: client, port: clientPort,
	})
	t.Cleanup(service.Stop)
	t.Cleanup(func() { _ = registrar.Close() })
	return registrar, service
}

func serveUDPUSSILifecycle(conn *net.UDPConn) error {
	invite, remote, err := readUDPUSSIRequest(conn, "INVITE")
	if err != nil {
		return err
	}
	if err := validateRegisteredUSSIHeaders(invite, "udp"); err != nil {
		return err
	}
	if err := writeUDPUSSIResponse(conn, remote, invite, nil); err != nil {
		return err
	}
	if _, _, err = readUDPUSSIRequest(conn, "ACK"); err != nil {
		return err
	}
	if _, err = conn.WriteToUDP([]byte(inboundUSSIINFO(invite, testUSSIMenuXML)), remote); err != nil {
		return err
	}
	info, remote, err := readOverlappingUDPUSSIInfo(conn)
	if err != nil {
		return err
	}
	if err := writeUDPUSSIResponse(conn, remote, info, []byte(testUSSIFinalXML)); err != nil {
		return err
	}
	secondInvite, remote, err := readUDPUSSIRequest(conn, "INVITE")
	if err != nil {
		return err
	}
	if err := writeUDPUSSIResponse(conn, remote, secondInvite, []byte(testUSSIMenuXML)); err != nil {
		return err
	}
	if _, _, err = readUDPUSSIRequest(conn, "ACK"); err != nil {
		return err
	}
	bye, remote, err := readUDPUSSIRequest(conn, "BYE")
	if err != nil {
		return err
	}
	return writeUDPUSSIResponse(conn, remote, bye, nil)
}

func readOverlappingUDPUSSIInfo(conn *net.UDPConn) (string, *net.UDPAddr, error) {
	var info string
	var infoRemote *net.UDPAddr
	responseReceived := false
	for info == "" || !responseReceived {
		message, remote, err := readUDPUSSIMessage(conn)
		if err != nil {
			return "", nil, err
		}
		if strings.HasPrefix(message, "SIP/2.0 200 ") {
			responseReceived = true
			continue
		}
		if method := sipRequestMethod(message); method != "INFO" {
			return "", nil, fmt.Errorf("overlapping USSI method = %q, want INFO", method)
		}
		info, infoRemote = message, remote
	}
	return info, infoRemote, nil
}

func serveUDPUSSITimeout(conn *net.UDPConn) error {
	invite, remote, err := readUDPUSSIRequest(conn, "INVITE")
	if err != nil {
		return err
	}
	if err := writeUDPUSSIResponse(conn, remote, invite, nil); err != nil {
		return err
	}
	if _, _, err = readUDPUSSIRequest(conn, "ACK"); err != nil {
		return err
	}
	bye, remote, err := readUDPUSSIRequest(conn, "BYE")
	if err != nil {
		return err
	}
	return writeUDPUSSIResponse(conn, remote, bye, nil)
}

func serveUDPUSSIUntilACK(conn *net.UDPConn) error {
	invite, remote, err := readUDPUSSIRequest(conn, "INVITE")
	if err != nil {
		return err
	}
	if err := writeUDPUSSIResponse(conn, remote, invite, nil); err != nil {
		return err
	}
	_, _, err = readUDPUSSIRequest(conn, "ACK")
	return err
}

func readUDPUSSIRequest(conn *net.UDPConn, method string) (string, *net.UDPAddr, error) {
	request, remote, err := readUDPUSSIMessage(conn)
	if err != nil {
		return "", nil, err
	}
	if actual := sipRequestMethod(request); actual != method {
		return "", nil, fmt.Errorf("USSI method = %q, want %q", actual, method)
	}
	return request, remote, nil
}

func readUDPUSSIMessage(conn *net.UDPConn) (string, *net.UDPAddr, error) {
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return "", nil, err
	}
	buffer := make([]byte, 64*1024)
	n, remote, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return "", nil, err
	}
	return string(buffer[:n]), remote, nil
}

func writeUDPUSSIResponse(conn *net.UDPConn, remote *net.UDPAddr, request string, body []byte) error {
	response := ussiResponseWire(request, body, conn.LocalAddr().String())
	_, err := conn.WriteToUDP([]byte(response), remote)
	return err
}

func ussiResponseWire(request string, body []byte, contactAddress string) string {
	to := rawSIPHeaderValue(request, "To")
	if !strings.Contains(strings.ToLower(to), ";tag=") {
		to += ";tag=remote"
	}
	var extra strings.Builder
	if sipRequestMethod(request) == "INVITE" {
		fmt.Fprintf(&extra, "Contact: <sip:ussi@%s>\r\n", contactAddress)
	}
	if len(body) > 0 {
		extra.WriteString("Content-Type: application/vnd.3gpp.ussd+xml\r\n")
	}
	return fmt.Sprintf(
		"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\n%sContent-Length: %d\r\n\r\n%s",
		rawSIPHeaderValue(request, "Via"), rawSIPHeaderValue(request, "From"), to,
		rawSIPHeaderValue(request, "Call-ID"), rawSIPHeaderValue(request, "CSeq"),
		extra.String(), len(body), body,
	)
}

func inboundUSSIINFO(invite, body string) string {
	return fmt.Sprintf(
		"INFO sip:registered-contact@ims.example SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP pcscf.ims.example;branch=z9hG4bKinbound-ussi\r\n"+
			"From: %s;tag=remote\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: 2 INFO\r\n"+
			"Info-Package: g.3gpp.ussd\r\nContent-Type: application/vnd.3gpp.ussd+xml\r\n"+
			"Content-Length: %d\r\n\r\n%s",
		rawSIPHeaderValue(invite, "To"), rawSIPHeaderValue(invite, "From"),
		rawSIPHeaderValue(invite, "Call-ID"), len(body), body,
	)
}

func validateRegisteredUSSIHeaders(request, transport string) error {
	if from := rawSIPHeaderValue(request, "From"); !strings.HasPrefix(from, "<sip:registered@ims.example>") {
		return fmt.Errorf("USSI From = %q", from)
	}
	contact := rawSIPHeaderValue(request, "Contact")
	if !strings.Contains(contact, "sip:registered-contact@") ||
		!strings.Contains(contact, ";transport="+transport) {
		return fmt.Errorf("USSI Contact = %q", contact)
	}
	if route := rawSIPHeaderValue(request, "Route"); route != "<sip:pcscf.ims.example;lr>" {
		return fmt.Errorf("USSI Route = %q", route)
	}
	return nil
}
