package imscore

import (
	"bufio"
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

type busyAKAProvider struct{}

func (busyAKAProvider) CalculateAKA(_, _ []byte) (enginesim.AKAResult, error) {
	return enginesim.AKAResult{}, enginesim.ErrAPDUBusy
}

func TestRegisterTransportCandidatesMatchOriginalOrder(t *testing.T) {
	tests := map[string][]string{
		"tcp":     {"tcp", "udp"},
		"udp":     {"udp"},
		"auto":    {"udp", "tcp"},
		"":        {"udp", "tcp"},
		"unknown": {"udp", "tcp"},
	}
	for configured, want := range tests {
		if got := registerTransportCandidates(configured); !reflect.DeepEqual(got, want) {
			t.Errorf("registerTransportCandidates(%q) = %v, want %v", configured, got, want)
		}
	}
}

func TestRegistrationStatusTransitionsMatchOriginalFSM(t *testing.T) {
	service := &Service{}
	if !service.transitionRegStatus(registrationRegistered) {
		t.Fatal("Unregistered -> Registered rejected")
	}
	if service.transitionRegStatus(registrationUnregistered) {
		t.Fatal("Registered -> Unregistered accepted")
	}
	if !service.transitionRegStatus(registrationRejectedTemporary) ||
		!service.transitionRegStatus(registrationStopping) ||
		!service.transitionRegStatus(registrationStopped) {
		t.Fatal("valid shutdown transition rejected")
	}
	if service.transitionRegStatus(registrationRegistered) {
		t.Fatal("Stopped -> Registered accepted")
	}
}

func TestServiceIsRegisteredUsesOriginalFSMDuringRefresh(t *testing.T) {
	service := &Service{regState: regRegistering}
	service.regStatus.Store(registrationRegistered)
	if !service.IsRegistered() {
		t.Fatal("registered FSM state was hidden by compatibility refresh state")
	}
}

func TestRegistrationStatusText(t *testing.T) {
	want := []string{"Unregistered", "Registered", "RejectedTemporary", "RejectedPermanent", "Stopping", "Stopped"}
	for status, text := range want {
		if got := registrationStatusText(int32(status)); got != text {
			t.Errorf("registrationStatusText(%d) = %q, want %q", status, got, text)
		}
	}
	if got := registrationStatusText(99); got != "Unknown" {
		t.Fatalf("registrationStatusText(99) = %q", got)
	}
}

func TestSplitRegistrarCandidatesStableDeduplication(t *testing.T) {
	want := []string{"pcscf-a.example:5060", "pcscf-b.example:5060"}
	got := splitRegistrarCandidates(" pcscf-a.example:5060 ; ; pcscf-b.example:5060;pcscf-a.example:5060 ")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitRegistrarCandidates = %v, want %v", got, want)
	}
}

func TestSelectRegisterAttemptRegistrarNormalizesIndex(t *testing.T) {
	cfg := IMSConfig{Registrar: "pcscf-a.example:5060;pcscf-b.example:5060"}
	selected, candidates, index, err := selectRegisterAttemptRegistrar(cfg, nil, 9)
	if err != nil {
		t.Fatal(err)
	}
	if selected != "pcscf-a.example:5060" || index != 0 || len(candidates) != 2 {
		t.Fatalf("selection = %q, %v, %d", selected, candidates, index)
	}
}

func TestRecoveredRegisterHelpers(t *testing.T) {
	if got := formatAORForSIP("", "user", "ims.example"); got != "sip:user@ims.example" {
		t.Fatalf("formatAORForSIP = %q", got)
	}
	if got := formatAORForSIP("TEL", "+44123", "ignored"); got != "tel:+44123" {
		t.Fatalf("formatAORForSIP tel = %q", got)
	}
	if got := formatHostForSIP("[2001:db8::1]"); got != "[2001:db8::1]" {
		t.Fatalf("formatHostForSIP = %q", got)
	}
	if got := pickAuthRealm("configured", " challenge ", "fallback"); got != "challenge" {
		t.Fatalf("pickAuthRealm = %q", got)
	}
	if got := securityClientMechanismCount("a,,b"); got != 3 {
		t.Fatalf("securityClientMechanismCount = %d", got)
	}
	if got := registerHeaderPortForTemplate(5070, 0, policy.IMSRegisterTemplate{}); got != 5070 {
		t.Fatalf("registerHeaderPortForTemplate = %d", got)
	}
}

func TestRegisterUsesCarrierFixedPANI(t *testing.T) {
	service, err := New(&IMSConfig{
		IMPI: "user@ims.example", IMPU: "sip:user@ims.example", Domain: "ims.example",
		IMSRegisterTemplate: policy.IMSRegisterTemplate{ID: "fixed", FixedPANI: "3GPP-NR-FDD;fixed=1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.StopCurrent()
	if got := service.GetPAccessNetworkInfo(); got != "3GPP-NR-FDD;fixed=1" {
		t.Fatalf("GetPAccessNetworkInfo = %q", got)
	}
}

func TestParseRecoveredRegisterResponseFields(t *testing.T) {
	response := &sipResponse{Headers: map[string]string{
		"Expires": "3600", "Contact": "<sip:a@example>;expires=120",
		"Retry-After": "9", "Min-Expires": "600",
	}}
	if got := parseRegisterExpiresFromResponse(response, 10); got != 3600 {
		t.Fatalf("parseRegisterExpiresFromResponse = %d", got)
	}
	retryAfter, minExpires := parseRegisterRetryHintsFromResponse(response)
	if retryAfter != 9*time.Second || minExpires != 600 {
		t.Fatalf("retry hints = %s, %d", retryAfter, minExpires)
	}
	if got := parseRemoteIPFromPath("<sip:user@[2001:db8::10]:5060;lr>"); got != "2001:db8::10" {
		t.Fatalf("parseRemoteIPFromPath = %q", got)
	}
}

func TestExtractChallengePrefersWWWAuthenticate(t *testing.T) {
	service := &Service{}
	www := strings.TrimPrefix(strings.TrimSpace(digestChallengeHeader(0x11, 0x22)), "WWW-Authenticate: ")
	www = strings.Replace(www, `realm="ims.example"`, `realm="www"`, 1)
	proxy := strings.TrimPrefix(strings.TrimSpace(digestChallengeHeader(0x33, 0x44)), "WWW-Authenticate: ")
	proxy = strings.Replace(proxy, `realm="ims.example"`, `realm="proxy"`, 1)
	response := &sipResponse{StatusCode: 407, Headers: map[string]string{
		"WWW-Authenticate": www, "Proxy-Authenticate": proxy,
	}}
	challenge, err := service.extractChallenge(response, response.StatusCode)
	if err != nil {
		t.Fatal(err)
	}
	if challenge.Realm != "www" {
		t.Fatalf("challenge = %+v", challenge)
	}
}

func TestRecoveredRegisterFailurePolicy(t *testing.T) {
	registerPolicy := policy.DefaultIMSRegisterPolicy()
	if !isTemporaryRegisterSIPResponse(registerPolicy, 503) ||
		!isForbiddenRegisterSIPResponse(registerPolicy, 403) {
		t.Fatal("default REGISTER failure policy was not restored")
	}
	if got := temporaryRegisterRetryInterval(registerPolicy); got != time.Minute {
		t.Fatalf("temporaryRegisterRetryInterval = %s", got)
	}
	if got := normalizeRegisterPolicySource("external"); got != "default" {
		t.Fatalf("normalizeRegisterPolicySource = %q", got)
	}
	if got := effectiveRegisterPolicyID(policy.IMSRegisterTemplate{}, " carrier-source "); got != "carrier-source" {
		t.Fatalf("effectiveRegisterPolicyID = %q", got)
	}
	retryPolicy := defaultRegisterRetryPolicy(registerPolicy)
	if retryPolicy.maxAuthChallenges != maxAKAChallenges {
		t.Fatalf("maxAuthChallenges = %d, want %d", retryPolicy.maxAuthChallenges, maxAKAChallenges)
	}
	if !retryPolicy.ShouldRetryDefaultInitial(403, "", "") ||
		retryPolicy.ShouldRetryDefaultInitial(403, "warning", "") {
		t.Fatal("default initial retry decision differs from recovered policy")
	}
}

func TestRegisterRetriesRejectedInitialRequestWithFallbackTemplate(t *testing.T) {
	config := registerTransportTestConfig("udp", "127.0.0.1:5060")
	config.IMSRegisterTemplate = policy.DefaultIMSRegisterTemplate()
	service, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer service.StopCurrent()
	requests := make(chan string, 2)
	service.transport.SetSendFn(func(request string) error {
		requests <- request
		status := 403
		if len(requests) == 2 {
			status = 200
		}
		service.transport.DeliverResponse(registerResponseForRequest(request, status, nil))
		return nil
	})
	if err := service.Register(context.Background()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	first, second := <-requests, <-requests
	if parseCSeq(sipHeaderValue(first, "CSeq")) != 2 || parseCSeq(sipHeaderValue(second, "CSeq")) != 3 {
		t.Fatalf("fallback CSeq sequence = %q, %q", sipHeaderValue(first, "CSeq"), sipHeaderValue(second, "CSeq"))
	}
	service.mu.RLock()
	template := service.regSession.template
	service.mu.RUnlock()
	if template.ID != "minimal_generic" || template.EnableInitialRejectFallback {
		t.Fatalf("fallback template = %+v", template)
	}
	if service.callID == "" || service.fromTag == "" || service.cseq.Load() != 3 ||
		service.lastSIPCode.Load() != 200 {
		t.Fatalf("registration runtime = call-id %q, tag %q, cseq %d, code %d",
			service.callID, service.fromTag, service.cseq.Load(), service.lastSIPCode.Load())
	}
}

func TestDecideRegisterFailureOutcome(t *testing.T) {
	now := time.Unix(100, 0)
	registerPolicy := policy.DefaultIMSRegisterPolicy()
	temporary := decideRegisterFailureOutcome(now, registerAttemptResult{statusCode: 503}, registerPolicy, false)
	if temporary.kind != registrationRejectedTemporary || temporary.reason != "temporary" ||
		temporary.nextRegister != now.Add(time.Minute) {
		t.Fatalf("temporary outcome = %+v", temporary)
	}
	forbidden := decideRegisterFailureOutcome(now, registerAttemptResult{statusCode: 403}, registerPolicy, false)
	if forbidden.kind != registrationRejectedPermanent || forbidden.reason != "forbidden" ||
		forbidden.nextRegister != now.Add(5*time.Minute) {
		t.Fatalf("forbidden outcome = %+v", forbidden)
	}
	retryAfter := decideRegisterFailureOutcome(now, registerAttemptResult{
		statusCode: 503, retryAfter: 17 * time.Second,
	}, registerPolicy, false)
	if retryAfter.reason != "retry_after" || retryAfter.nextRegister != now.Add(17*time.Second) {
		t.Fatalf("retry-after outcome = %+v", retryAfter)
	}
}

func TestRegisterTransportDeadlineUsesEarlierContextDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(100*time.Millisecond))
	defer cancel()
	deadline := registerTransportDeadline(ctx, time.Minute)
	contextDeadline, _ := ctx.Deadline()
	if !deadline.Equal(contextDeadline) {
		t.Fatalf("deadline = %s, want %s", deadline, contextDeadline)
	}
}

func TestRefreshRegisterCarriesLearnedServiceRoute(t *testing.T) {
	service := &Service{cfg: &IMSConfig{
		IMPI: "user@ims.example", IMPU: "sip:user@ims.example", Domain: "ims.example",
		LocalIP: net.ParseIP("192.0.2.10"), LocalPort: 5060, Transport: "udp",
	}}
	request := service.buildRegister(&registerSession{
		callID: "call-1", fromTag: "tag-1", contactUser: "contact-1", cseq: 3,
		serviceRoute: "<sip:pcscf.ims.example;lr>",
	}, "Digest response")
	if got := sipHeaderValue(request, "Route"); got != "<sip:pcscf.ims.example;lr>" {
		t.Fatalf("refresh REGISTER Route = %q", got)
	}
}

func TestServiceStatusRestoresRegistrationDiagnostics(t *testing.T) {
	service, err := New(registerTransportTestConfig("udp", "127.0.0.1:5060"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.StopCurrent()
	service.registrar = "pcscf.example:5060"
	service.registrarCandidates = []string{"pcscf.example:5060", "pcscf2.example:5060"}
	service.registrarSource = "registrar"
	service.lastSIPCode.Store(503)
	service.lastSIPText = "Service Unavailable"
	service.transitionRegStatus(registrationRejectedTemporary)
	status := service.StatusCurrent()
	if status.RegStatus != "RejectedTemporary" || status.IMPU != service.cfg.IMPU ||
		status.LastSIPCode != 503 || status.RegistrarSource != "registrar" {
		t.Fatalf("status = %+v", status)
	}
	status.RegStatus = "Registered"
	if !status.IsRegistered() {
		t.Fatal("ServiceStatus.IsRegistered ignored recovered RegStatus")
	}
	if got := status.ToMap()["registrar_candidates"]; !reflect.DeepEqual(got, status.RegistrarCandidates) {
		t.Fatalf("ToMap registrar_candidates = %v", got)
	}
}

func TestRegisterAPDUBusySchedulesThreeSecondRetry(t *testing.T) {
	config := registerTransportTestConfig("udp", "127.0.0.1:5060")
	config.AKAProvider = busyAKAProvider{}
	service, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer service.StopCurrent()
	service.transport.SetSendFn(func(request string) error {
		challenge := strings.TrimPrefix(strings.TrimSpace(digestChallengeHeader(0x11, 0x22)), "WWW-Authenticate: ")
		service.transport.DeliverResponse(registerResponseForRequest(request, 401, map[string]string{
			"WWW-Authenticate": challenge,
		}))
		return nil
	})
	started := time.Now()
	err = service.Register(context.Background())
	if !errors.Is(err, enginesim.ErrAPDUBusy) {
		t.Fatalf("Register error = %v", err)
	}
	service.mu.RLock()
	next := service.nextRegister
	service.mu.RUnlock()
	if next.Before(started.Add(2900*time.Millisecond)) || next.After(started.Add(3100*time.Millisecond)) {
		t.Fatalf("next register = %s, started = %s", next, started)
	}
}

func TestRegisterFailureTriggersOneReconnectAtATime(t *testing.T) {
	service, err := New(registerTransportTestConfig("udp", "127.0.0.1:5060"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.StopCurrent()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	service.OnReconnectNeeded = func() {
		entered <- struct{}{}
		<-release
	}
	service.triggerRegisterReconnect()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("reconnect callback was not invoked")
	}
	service.triggerRegisterReconnect()
	if len(entered) != 0 {
		t.Fatal("concurrent reconnect callback was invoked")
	}
	close(release)
}

func TestCanceledRegisterDoesNotTriggerFailureFSMOrReconnect(t *testing.T) {
	service, err := New(registerTransportTestConfig("udp", "127.0.0.1:5060"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.StopCurrent()
	service.transport.SetSendFn(func(string) error { return nil })
	reconnect := make(chan struct{}, 1)
	service.OnReconnectNeeded = func() { reconnect <- struct{}{} }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.Register(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Register error = %v", err)
	}
	if got := service.regStatus.Load(); got != registrationUnregistered {
		t.Fatalf("registration status = %s", registrationStatusText(got))
	}
	select {
	case <-reconnect:
		t.Fatal("canceled REGISTER triggered reconnect")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestRegisterTCPDoesNotRetryUDPAfterAuthenticationChallenge(t *testing.T) {
	tcpRegistrar, udpRegistrar := listenRegisterTransports(t)
	defer tcpRegistrar.Close()
	defer udpRegistrar.Close()
	serverResult := make(chan error, 1)
	go serveChallengedTCPRegister(tcpRegistrar, serverResult)

	service, err := New(registerTransportTestConfig("tcp", tcpRegistrar.Addr().String()))
	if err != nil {
		t.Fatal(err)
	}
	defer service.StopCurrent()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = service.Register(ctx)
	if err == nil || !strings.Contains(err.Error(), "status 503") {
		t.Fatalf("Register error = %v", err)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
	assertNoUDPRegister(t, udpRegistrar)
}

func TestRegisterRetries423WithMinExpiresOnSameTransport(t *testing.T) {
	registrar, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer registrar.Close()
	requests := make(chan string, 2)
	go serveMinExpiresSequence(registrar, requests)
	service, err := New(registerTransportTestConfig("udp", registrar.LocalAddr().String()))
	if err != nil {
		t.Fatal(err)
	}
	defer service.StopCurrent()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.Register(ctx); err != nil {
		t.Fatalf("Register: %v", err)
	}
	<-requests
	second := <-requests
	if got := sipHeaderValue(second, "Expires"); got != "7200" {
		t.Fatalf("second REGISTER Expires = %q", got)
	}
}

func TestTemporaryFailureAdvancesRegistrarForNextRegister(t *testing.T) {
	first, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	firstSeen := make(chan string, 1)
	secondSeen := make(chan string, 1)
	go serveRegisterStatus(first, 503, firstSeen)
	go serveRegisterStatus(second, 200, secondSeen)
	registrars := first.LocalAddr().String() + ";" + second.LocalAddr().String()
	service, err := New(registerTransportTestConfig("udp", registrars))
	if err != nil {
		t.Fatal(err)
	}
	defer service.StopCurrent()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := service.Register(ctx); err == nil || !strings.Contains(err.Error(), "status 503") {
		t.Fatalf("first Register error = %v", err)
	}
	if err := service.Register(ctx); err != nil {
		t.Fatalf("second Register: %v", err)
	}
	if <-firstSeen == "" || <-secondSeen == "" {
		t.Fatal("REGISTER requests did not reach both registrar candidates")
	}
}

func TestRegisterNetworkFailureAdvancesRegistrar(t *testing.T) {
	service, err := New(registerTransportTestConfig("udp", "pcscf-a.example:5060;pcscf-b.example:5060"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.StopCurrent()
	service.mu.Lock()
	service.registrar = "pcscf-a.example:5060"
	service.registrarCandidates = []string{"pcscf-a.example:5060", "pcscf-b.example:5060"}
	service.registrarSource = "registrar"
	service.mu.Unlock()
	service.transport.SetSendFn(func(string) error { return errors.New("network unavailable") })
	if err := service.Register(context.Background()); err == nil || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("Register error = %v", err)
	}
	service.mu.RLock()
	registrar, index := service.registrar, service.registrarIndex
	service.mu.RUnlock()
	if registrar != "pcscf-b.example:5060" || index != 1 {
		t.Fatalf("registrar after network failure = %q index %d", registrar, index)
	}
}

func serveMinExpiresSequence(registrar *net.UDPConn, requests chan<- string) {
	buffer := make([]byte, 64*1024)
	for attempt := 0; attempt < 2; attempt++ {
		n, remote, err := registrar.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		request := string(buffer[:n])
		requests <- request
		status, headers := 423, "Min-Expires: 7200\r\n"
		if attempt == 1 {
			status, headers = 200, "Expires: 7200\r\n"
		}
		_, _ = registrar.WriteToUDP([]byte(registerWireResponse(request, status, headers)), remote)
	}
}

func serveChallengedTCPRegister(listener *net.TCPListener, result chan<- error) {
	conn, err := listener.AcceptTCP()
	if err != nil {
		result <- err
		return
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	initial, err := readSIPStreamMessage(reader)
	if err != nil {
		result <- err
		return
	}
	challenge := strings.TrimPrefix(strings.TrimSpace(digestChallengeHeader(0x11, 0x22)), "WWW-Authenticate: ")
	if _, err = conn.Write([]byte(registerWireResponse(initial, 401, "WWW-Authenticate: "+challenge+"\r\n"))); err != nil {
		result <- err
		return
	}
	authenticated, err := readSIPStreamMessage(reader)
	if err == nil {
		_, err = conn.Write([]byte(registerWireResponse(authenticated, 503, "")))
	}
	result <- err
}
