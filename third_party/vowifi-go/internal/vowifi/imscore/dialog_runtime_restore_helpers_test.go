package imscore

import (
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
)

func testClientDialog(t *testing.T, service *Service, callID string) *imscoreDialogHandle {
	t.Helper()
	request := mustClientInviteRequest(t, callID)
	response := testReliableInviteResponse(t, request.String(), 200).parsed
	dialog := newClientDialogHandle(request, response)
	service.storeClientDialog(dialog, request, response)
	return dialog
}

func testServerDialog(t *testing.T, service *Service, callID string) *imscoreDialogHandle {
	t.Helper()
	raw := fmt.Sprintf("INVITE sip:user@ims.example SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP peer.example;branch=z9hG4bKserver\r\n"+
		"From: <sip:peer@ims.example>;tag=remote\r\n"+
		"To: <sip:user@ims.example>\r\n"+
		"Call-ID: %s\r\nCSeq: 1 INVITE\r\n"+
		"Contact: <sip:peer@192.0.2.20:5060>\r\nContent-Length: 0\r\n\r\n", callID)
	request := mustServerTestRequest(t, raw)
	response := buildInboundResponseFromRequest(request, 200, "OK", nil, nil)
	response.AppendHeader(testContactHeader(t, "sip:user@192.0.2.10:5060"))
	dialog := newServerDialogHandle(request, response)
	service.storeServerDialog(dialog, request)
	return dialog
}

func newRegisteredUDPDialogService(
	t *testing.T,
) (*Service, *net.UDPConn, *imscoreDialogHandle) {
	t.Helper()
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
	remote := registrar.LocalAddr().(*net.UDPAddr)
	service.mu.Lock()
	service.regState = regRegistered
	service.registrationIO = client
	service.registrationRemote = cloneUDPAddr(remote)
	service.registrationTransport = "udp"
	service.mu.Unlock()
	service.regStatus.Store(registrationRegistered)
	service.activateInitialSendAndReceive(&initialRegistrationTransport{
		kind: "udp", remote: remote, packet: client,
		port: client.LocalAddr().(*net.UDPAddr).Port,
	})
	t.Cleanup(service.Stop)
	return service, registrar, testUDPClientDialog(t, service)
}

func testUDPClientDialog(t *testing.T, service *Service) *imscoreDialogHandle {
	t.Helper()
	raw := transactionRequestWithTransport("INVITE", "dialog-production-udp", "UDP")
	raw = strings.Replace(raw, "Content-Length: 0", "Contact: <sip:user@127.0.0.1:5060>\r\nContent-Length: 0", 1)
	message, err := parseSIPMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	request := message.(*sip.Request)
	response := testReliableInviteResponse(t, request.String(), 200).parsed
	dialog := newClientDialogHandle(request, response)
	service.storeClientDialog(dialog, request, response)
	return dialog
}

func serveOneDialogTransaction(registrar *net.UDPConn, result chan<- error) {
	buffer := make([]byte, 64*1024)
	if err := registrar.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		result <- err
		return
	}
	n, remote, err := registrar.ReadFromUDP(buffer)
	if err != nil {
		result <- err
		return
	}
	request := string(buffer[:n])
	if sipRequestMethod(request) != "INFO" {
		result <- fmt.Errorf("dialog request method = %q", sipRequestMethod(request))
		return
	}
	response := transactionResponseWire(request, 200, "OK")
	_, err = registrar.WriteToUDP([]byte(response), remote)
	result <- err
}

func testReliableInviteResponse(t *testing.T, request string, status int) *sipResponse {
	t.Helper()
	response := mustTransactionResponse(t, request, status)
	response.parsed.AppendHeader(testContactHeader(t, "sip:callee@192.0.2.20:5060"))
	response.parsed.AppendHeader(sip.NewHeader("Record-Route", "<sip:edge-a.example;lr>"))
	response.parsed.AppendHeader(sip.NewHeader("Record-Route", "<sip:edge-b.example;lr>"))
	return newSIPResponse(response.parsed)
}

func setResponseToTag(response *sip.Response, tag string) {
	response.To().Params.Remove("tag")
	response.To().Params.Add("tag", tag)
}

func testContactHeader(t *testing.T, value string) *sip.ContactHeader {
	t.Helper()
	var uri sip.Uri
	if err := sip.ParseUri(value, &uri); err != nil {
		t.Fatal(err)
	}
	return &sip.ContactHeader{Address: uri}
}

func testDialogTemplate(t *testing.T, method sip.RequestMethod) *sip.Request {
	t.Helper()
	request := sip.NewRequest(method, sip.Uri{})
	request.AppendHeader(sip.NewHeader("Via", "SIP/2.0/UDP attacker.example;branch=z9hG4bKbad"))
	request.AppendHeader(sip.NewHeader("From", "<sip:wrong@example>;tag=wrong"))
	request.AppendHeader(sip.NewHeader("To", "<sip:wrong@example>;tag=wrong"))
	request.AppendHeader(sip.NewHeader("Call-ID", "wrong"))
	request.AppendHeader(sip.NewHeader("CSeq", "99 "+string(method)))
	request.AppendHeader(sip.NewHeader("Route", "<sip:wrong.example;lr>"))
	request.AppendHeader(sip.NewHeader("X-Dialog-Test", "preserved"))
	request.SetBody([]byte("payload"))
	return request
}

func assertRestoredDialogRequest(
	t *testing.T,
	request string,
	dialog *imscoreDialogHandle,
	wantCSeq string,
) {
	t.Helper()
	checks := map[string]string{
		"Call-ID": dialog.callID, "CSeq": wantCSeq,
		"X-Dialog-Test": "preserved",
	}
	for name, want := range checks {
		if got := rawSIPHeaderValue(request, name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	if got := rawSIPHeaderValue(request, "From"); !strings.Contains(got, "tag="+dialog.fromTag) {
		t.Fatalf("From = %q", got)
	}
	if got := rawSIPHeaderValue(request, "To"); !strings.Contains(got, "tag="+dialog.toTag) {
		t.Fatalf("To = %q", got)
	}
	routes := allRawSIPHeaderValues(request, "Route")
	wantRoutes := []string{"<sip:edge-b.example;lr>", "<sip:edge-a.example;lr>"}
	if !reflect.DeepEqual(routes, wantRoutes) {
		t.Fatalf("Route = %#v, want %#v", routes, wantRoutes)
	}
}

func testInboundDialogRequest(method string, dialog *imscoreDialogHandle, cseq uint32) string {
	return fmt.Sprintf("%s sip:user@ims.example SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP peer.example;branch=z9hG4bKinbound\r\n"+
		"From: <sip:peer@ims.example>;tag=%s\r\n"+
		"To: <sip:user@ims.example>;tag=%s\r\n"+
		"Call-ID: %s\r\nCSeq: %d %s\r\nContent-Length: 0\r\n\r\n",
		method, dialog.toTag, dialog.fromTag, dialog.callID, cseq, method)
}

func allRawSIPHeaderValues(raw, name string) []string {
	values := make([]string, 0, 2)
	for _, line := range strings.Split(raw, "\r\n") {
		headerName, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(headerName), name) {
			values = append(values, strings.TrimSpace(value))
		}
	}
	return values
}
