package imscore

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
)

func TestRegisterUsesConfiguredIMSNetworkTransport(t *testing.T) {
	registrar, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer registrar.Close()
	requestSeen := make(chan string, 1)
	go serveRegisterStatus(registrar, 200, requestSeen)

	svc, err := New(&IMSConfig{
		DeviceID: "dev-1", IMEI: "356938035643809", IMSI: "310260123456789", IMPI: "310260123456789@ims.example",
		IMPU: []string{"sip:310260123456789@ims.example"}, Domain: "ims.example",
		LocalIP: net.IPv4(127, 0, 0, 1), Transport: "udp", Registrar: registrar.LocalAddr().String(),
		IMSNetwork: NewSystemIMSNetwork(net.IPv4(127, 0, 0, 1)), AKAProvider: stubAKAProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Register(ctx); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !svc.IsRegistered() {
		t.Fatal("service did not enter registered state")
	}
	select {
	case request := <-requestSeen:
		if !strings.HasPrefix(request, "REGISTER sip:ims.example SIP/2.0") {
			t.Fatalf("unexpected request: %q", request)
		}
		if strings.Contains(request, "@ims.example@") {
			t.Fatalf("REGISTER contains a duplicated identity domain: %q", request)
		}
		if !strings.Contains(request, "From: <sip:310260123456789@ims.example>") ||
			!strings.Contains(request, "Contact: <sip:310260123456789@127.0.0.1:") {
			t.Fatalf("REGISTER identity URIs are invalid: %q", request)
		}
		if !strings.Contains(sipHeaderValue(request, "Contact"), `+sip.instance="<urn:gsma:imei:35693803-564380-9>"`) {
			t.Fatalf("REGISTER Contact omitted the IMEI instance URN: %q", sipHeaderValue(request, "Contact"))
		}
		authorization := sipHeaderValue(request, "Authorization")
		for _, field := range []string{`username="310260123456789@ims.example"`, `realm="ims.example"`, `nonce=""`, `uri="sip:ims.example"`, `response=""`} {
			if !strings.Contains(authorization, field) {
				t.Fatalf("initial Authorization omitted %s: %q", field, authorization)
			}
		}
		if got := sipHeaderValue(request, "P-Access-Network-Info"); !strings.Contains(got, "network-id=310026") || !strings.Contains(got, "country=US") {
			t.Fatalf("REGISTER P-Access-Network-Info = %q", got)
		}
	case <-ctx.Done():
		t.Fatal("registrar did not receive REGISTER")
	}
}

func TestRegistrationRefreshesBeforeExpiryAndReportsFailure(t *testing.T) {
	registrar, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer registrar.Close()
	requests := make(chan string, 2)
	go serveRegistrationSequence(registrar, requests, []int{200, 403})

	svc, err := New(&IMSConfig{
		DeviceID: "dev-refresh", IMSI: "310260123456789", IMPI: "310260123456789@ims.example",
		IMPU: []string{"sip:310260123456789@ims.example"}, Domain: "ims.example",
		LocalIP: net.IPv4(127, 0, 0, 1), Transport: "udp", Registrar: registrar.LocalAddr().String(),
		IMSNetwork: NewSystemIMSNetwork(net.IPv4(127, 0, 0, 1)), AKAProvider: stubAKAProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := svc.Register(ctx); err != nil {
		t.Fatalf("Register: %v", err)
	}

	seen := make([]string, 0, 2)
	for attempt := 0; attempt < 2; attempt++ {
		select {
		case request := <-requests:
			seen = append(seen, request)
		case <-ctx.Done():
			t.Fatalf("received only %d REGISTER requests before timeout", attempt)
		}
	}
	if sipHeaderValue(seen[0], "Call-ID") != sipHeaderValue(seen[1], "Call-ID") {
		t.Fatal("registration refresh changed Call-ID")
	}
	if sipHeaderValue(seen[0], "CSeq") != "1 REGISTER" || sipHeaderValue(seen[1], "CSeq") != "2 REGISTER" {
		t.Fatalf("refresh CSeq values = %q, %q", sipHeaderValue(seen[0], "CSeq"), sipHeaderValue(seen[1], "CSeq"))
	}
	if !strings.Contains(sipHeaderValue(seen[1], "Contact"), ";expires=3600") {
		t.Fatalf("refresh Contact omitted expires: %q", sipHeaderValue(seen[1], "Contact"))
	}
	select {
	case err := <-svc.RegistrationErrors():
		if err == nil || !strings.Contains(err.Error(), "status 403") {
			t.Fatalf("refresh error = %v, want status 403", err)
		}
	case <-ctx.Done():
		t.Fatal("registration refresh failure was not reported")
	}
	if svc.IsRegistered() || svc.RegState() != regFailed {
		t.Fatalf("registration state after refresh failure = %q", svc.RegState())
	}
}

func TestRegistrationExpiresPrefersContactBinding(t *testing.T) {
	response := &sipResponse{Headers: map[string]string{
		"Contact": "<sip:user@10.0.0.2>;expires=120", "Expires": "3600",
	}}
	if got := registrationExpires(response, time.Hour); got != 120*time.Second {
		t.Fatalf("registrationExpires = %s, want 2m", got)
	}
}

type sqnSyncAKAProvider struct {
	calls int
	auts  []byte
}

func (p *sqnSyncAKAProvider) CalculateAKA(_, _ []byte) (AKAResult, error) {
	p.calls++
	if p.calls == 1 {
		return AKAResult{AUTS: append([]byte(nil), p.auts...)}, enginesim.ErrSyncFailure
	}
	return AKAResult{
		RES: bytes.Repeat([]byte{0x33}, 16),
		CK:  bytes.Repeat([]byte{0x11}, 16),
		IK:  bytes.Repeat([]byte{0x22}, 16),
	}, nil
}

func TestRegisterRecoversFromAKASQNSynchronizationFailure(t *testing.T) {
	registrar, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer registrar.Close()
	auts := bytes.Repeat([]byte{0xa5}, akaAUTSLength)
	serverResult := make(chan error, 1)
	go serveSQNSyncRegistrar(registrar, auts, serverResult)
	provider := &sqnSyncAKAProvider{auts: auts}
	svc, err := New(&IMSConfig{
		DeviceID: "dev-sync", IMSI: "310260123456789", IMPI: "310260123456789@ims.example",
		Domain: "ims.example", LocalIP: net.IPv4(127, 0, 0, 1), Transport: "udp",
		Registrar: registrar.LocalAddr().String(), IMSNetwork: NewSystemIMSNetwork(net.IPv4(127, 0, 0, 1)),
		AKAProvider: provider,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := svc.Register(ctx); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if provider.calls != 2 || !svc.IsRegistered() {
		t.Fatalf("AKA calls=%d registered=%t", provider.calls, svc.IsRegistered())
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestProcessAKAChallengeRejectsInvalidSynchronizationToken(t *testing.T) {
	challenge := digestChallengeForTest(0x11, 0x22)
	provider := &sqnSyncAKAProvider{auts: bytes.Repeat([]byte{0xa5}, akaAUTSLength-1)}
	if _, _, err := ProcessAKAChallengeWithResult(challenge, provider, "user", "REGISTER", "sip:ims.example"); err == nil || !strings.Contains(err.Error(), "AUTS length") {
		t.Fatalf("ProcessAKAChallengeWithResult error = %v", err)
	}
}

func TestRegisterLimitsAKAChallenges(t *testing.T) {
	svc, err := New(&IMSConfig{
		DeviceID: "dev-limit", IMSI: "310260123456789", IMPI: "310260123456789@ims.example",
		Domain: "ims.example", AKAProvider: stubAKAProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Stop()
	svc.transport.SetSendFn(func(request string) error {
		challenge := strings.TrimPrefix(strings.TrimSpace(digestChallengeHeader(0x11, 0x22)), "WWW-Authenticate: ")
		svc.transport.DeliverResponse(registerResponseForRequest(request, 401, map[string]string{
			"WWW-Authenticate": challenge,
		}))
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Register(ctx); err == nil || !strings.Contains(err.Error(), "AKA challenge limit 2 exceeded") {
		t.Fatalf("Register error = %v, want challenge limit", err)
	}
}

func TestReceiveResponseMatchesFullRegisterTransaction(t *testing.T) {
	transport := newSIPTransport()
	svc := &Service{transport: transport}
	session := &registerSession{callID: "call-1", cseq: 2, branch: "z9hG4bK-current"}
	transport.DeliverResponse(&sipResponse{StatusCode: 200, CallID: session.callID, CSeq: "1 REGISTER", Headers: map[string]string{
		"Via": "SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK-old",
	}})
	transport.DeliverResponse(&sipResponse{StatusCode: 403, CallID: session.callID, CSeq: "2 REGISTER", Headers: map[string]string{
		"Via": "SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK-wrong",
	}})
	transport.DeliverResponse(&sipResponse{StatusCode: 200, CallID: session.callID, CSeq: "2 REGISTER", Headers: map[string]string{
		"Via": "SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK-current",
	}})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := svc.receiveResponse(ctx, session)
	if err != nil {
		t.Fatalf("receiveResponse: %v", err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("matched stale REGISTER response status %d", response.StatusCode)
	}
}

func TestReceiveResponseRejectsMalformedCurrentTransaction(t *testing.T) {
	transport := newSIPTransport()
	svc := &Service{transport: transport}
	session := &registerSession{callID: "call-1", cseq: 2, branch: "z9hG4bK-current"}
	transport.DeliverResponse(&sipResponse{StatusCode: 200, CallID: session.callID})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := svc.receiveResponse(ctx, session); err == nil || !strings.Contains(err.Error(), "invalid REGISTER response CSeq") {
		t.Fatalf("receiveResponse error = %v, want malformed CSeq", err)
	}
}

func TestRegisterPropagatesRegistrarRejection(t *testing.T) {
	registrar, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer registrar.Close()
	go serveRegisterStatus(registrar, 403, nil)
	svc, err := New(&IMSConfig{
		DeviceID: "dev-1", IMSI: "310260123456789", IMPI: "310260123456789@ims.example",
		Domain: "ims.example", LocalIP: net.IPv4(127, 0, 0, 1), Transport: "udp",
		Registrar: registrar.LocalAddr().String(), IMSNetwork: NewSystemIMSNetwork(net.IPv4(127, 0, 0, 1)),
		IPSec3GPPEnabled: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Register(ctx); err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("Register error = %v, want 403", err)
	}
	if svc.IsRegistered() || svc.RegState() != regFailed {
		t.Fatalf("registration state = %q", svc.RegState())
	}
}

func TestRegistrationResponseErrorIncludesRegistrarDiagnostic(t *testing.T) {
	err := registrationResponseError(&sipResponse{
		StatusCode: 400,
		Reason:     "Bad Request",
		Headers:    map[string]string{"Warning": `399 pcscf.example "Malformed Contact"`},
	}, true)
	if got := err.Error(); !strings.Contains(got, "authenticated REGISTER") || !strings.Contains(got, "400 (Bad Request") || !strings.Contains(got, "Malformed Contact") {
		t.Fatalf("registrationResponseError = %q", got)
	}
}

func serveRegisterStatus(conn *net.UDPConn, status int, seen chan<- string) {
	buffer := make([]byte, 64*1024)
	n, remote, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return
	}
	request := string(buffer[:n])
	if seen != nil {
		seen <- request
	}
	response := registerWireResponse(request, status, "")
	_, _ = conn.WriteToUDP([]byte(response), remote)
}

func serveRegistrationSequence(conn *net.UDPConn, seen chan<- string, statuses []int) {
	buffer := make([]byte, 64*1024)
	for _, status := range statuses {
		n, remote, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		request := string(buffer[:n])
		seen <- request
		response := registerWireResponse(request, status, "Expires: 1\r\n")
		_, _ = conn.WriteToUDP([]byte(response), remote)
	}
}

func registerWireResponse(request string, status int, extraHeaders string) string {
	return fmt.Sprintf(
		"SIP/2.0 %d Test\r\nVia: %s\r\nCall-ID: %s\r\nCSeq: %s\r\n%sContent-Length: 0\r\n\r\n",
		status,
		sipHeaderValue(request, "Via"),
		sipHeaderValue(request, "Call-ID"),
		sipHeaderValue(request, "CSeq"),
		extraHeaders,
	)
}

func serveSQNSyncRegistrar(conn *net.UDPConn, auts []byte, result chan<- error) {
	buffer := make([]byte, 64*1024)
	for attempt := 0; attempt < 3; attempt++ {
		n, remote, err := conn.ReadFromUDP(buffer)
		if err != nil {
			result <- err
			return
		}
		request := string(buffer[:n])
		authorization := sipHeaderValue(request, "Authorization")
		if err := validateSQNSyncAuthorization(attempt, authorization, auts); err != nil {
			result <- err
			return
		}
		status, headers := 401, digestChallengeHeader(byte(0x11+attempt), byte(0x22+attempt))
		if attempt == 2 {
			status, headers = 200, ""
		}
		response := registerWireResponse(request, status, headers)
		if _, err := conn.WriteToUDP([]byte(response), remote); err != nil {
			result <- err
			return
		}
	}
	result <- nil
}

func validateSQNSyncAuthorization(attempt int, authorization string, auts []byte) error {
	switch attempt {
	case 0:
		for _, field := range []string{`username="310260123456789@ims.example"`, `nonce=""`, `response=""`} {
			if !strings.Contains(authorization, field) {
				return fmt.Errorf("initial REGISTER Authorization = %q, missing %s", authorization, field)
			}
		}
	case 1:
		want := `auts="` + base64.StdEncoding.EncodeToString(auts) + `"`
		if !strings.Contains(authorization, want) {
			return fmt.Errorf("synchronization REGISTER Authorization = %q, missing %s", authorization, want)
		}
	case 2:
		if authorization == "" || strings.Contains(authorization, "auts=") {
			return fmt.Errorf("fresh challenge Authorization = %q", authorization)
		}
	}
	return nil
}

func digestChallengeHeader(randByte, autnByte byte) string {
	nonce := base64.StdEncoding.EncodeToString(append(bytes.Repeat([]byte{randByte}, 16), bytes.Repeat([]byte{autnByte}, 16)...))
	return fmt.Sprintf("WWW-Authenticate: Digest realm=\"ims.example\", nonce=\"%s\", algorithm=AKAv1-MD5, qop=\"auth\"\r\n", nonce)
}

func digestChallengeForTest(randByte, autnByte byte) *DigestChallenge {
	nonce := base64.StdEncoding.EncodeToString(append(bytes.Repeat([]byte{randByte}, 16), bytes.Repeat([]byte{autnByte}, 16)...))
	challenge, _ := ParseDigestChallenge(fmt.Sprintf(`Digest realm="ims.example", nonce="%s", algorithm=AKAv1-MD5, qop="auth"`, nonce))
	return challenge
}

func sipHeaderValue(message, name string) string {
	for _, line := range strings.Split(message, "\r\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
