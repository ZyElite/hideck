package imscore

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFirstSIPHeaderURI(t *testing.T) {
	value := "<sip:+447840844894@o2.co.uk>,<tel:+447840844894>"
	if got := firstSIPHeaderURI(value); got != "sip:+447840844894@o2.co.uk" {
		t.Fatalf("firstSIPHeaderURI = %q", got)
	}
}

func TestBuildSIPRequestResponseAcknowledgesNotify(t *testing.T) {
	request := "NOTIFY sip:user@example SIP/2.0\r\n" +
		"Via: SIP/2.0/TCP 192.0.2.1:6060;branch=z9hG4bK-notify\r\n" +
		"From: <sip:server@example>;tag=server\r\n" +
		"To: <sip:user@example>\r\n" +
		"Call-ID: notify-call\r\n" +
		"CSeq: 1 NOTIFY\r\n" +
		"Event: reg\r\nContent-Length: 0\r\n\r\n"
	response, err := buildSIPRequestResponse(request, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"SIP/2.0 200 OK", "Via: SIP/2.0/TCP 192.0.2.1:6060;branch=z9hG4bK-notify",
		"Call-ID: notify-call", "CSeq: 1 NOTIFY", "To: <sip:user@example>;tag=",
	} {
		if !strings.Contains(response, want) {
			t.Fatalf("NOTIFY response omitted %q: %q", want, response)
		}
	}
}

func TestRegistrationSubscriptionUsesProductionTransactionAndRefreshes(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	client, server := net.Pipe()
	service.activateProtectedRegistrationTCP(client)
	t.Cleanup(func() { _ = server.Close() })

	requests := make(chan string, 2)
	errorsSeen := make(chan error, 1)
	go serveSubscriptionResponses(server, requests, errorsSeen, 2)
	for attempt := 0; attempt < 2; attempt++ {
		if err := service.sendSubscribeReg(context.Background()); err != nil {
			t.Fatalf("sendSubscribeReg attempt %d: %v", attempt+1, err)
		}
	}
	if err := <-errorsSeen; err != nil {
		t.Fatal(err)
	}
	first, second := <-requests, <-requests
	assertRegistrationSubscriptionRequest(t, first, 5)
	assertRegistrationSubscriptionRequest(t, second, 6)

	service.mu.RLock()
	expires := service.subscriptionExpires
	lastOK := service.subscriptionLastOKAt
	refreshAt := service.subscriptionRefreshAt
	service.mu.RUnlock()
	if expires != 120*time.Second {
		t.Fatalf("subscription expires = %s, want 2m", expires)
	}
	if delay := refreshAt.Sub(lastOK); delay != time.Minute {
		t.Fatalf("subscription refresh delay = %s, want 1m", delay)
	}
}

func serveSubscriptionResponses(
	conn net.Conn,
	requests chan<- string,
	result chan<- error,
	count int,
) {
	reader := bufio.NewReader(conn)
	for index := 0; index < count; index++ {
		request, err := readSIPStreamMessage(reader)
		if err != nil {
			result <- err
			return
		}
		requests <- request
		if _, err = io.WriteString(conn, registerWireResponse(request, 200, "Expires: 120\r\n")); err != nil {
			result <- err
			return
		}
	}
	result <- nil
}

func assertRegistrationSubscriptionRequest(t *testing.T, request string, wantCSeq int) {
	t.Helper()
	if !strings.HasPrefix(request, "SUBSCRIBE sip:+447840844894@o2.co.uk SIP/2.0") {
		t.Fatalf("request line = %q", strings.SplitN(request, "\r\n", 2)[0])
	}
	checks := map[string]string{
		"Event": "reg", "Accept": reginfoContentType, "Expires": "3600",
		"CSeq":            strconv.Itoa(wantCSeq) + " SUBSCRIBE",
		"Security-Verify": "ipsec-3gpp;alg=hmac-sha-1-96",
	}
	for name, want := range checks {
		if got := rawSIPHeaderValue(request, name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	contact := rawSIPHeaderValue(request, "Contact")
	if !strings.Contains(contact, ":16083;transport=tcp>") {
		t.Fatalf("Contact = %q, want protected server port and transport", contact)
	}
	viaBranch, err := parseTopViaBranch(rawSIPHeaderValue(request, "Via"))
	if err != nil || !strings.HasPrefix(viaBranch, "z9hG4bK") || len(viaBranch) != len("z9hG4bK")+36 {
		t.Fatalf("Via branch = %q, err = %v", viaBranch, err)
	}
}

func TestRegistrationSubscriptionSkipReason(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	if eligible, reason := service.registrationSubscriptionGate(); eligible || reason != "no_registration_tcp" {
		t.Fatalf("gate without TCP = eligible=%v reason=%q", eligible, reason)
	}
}

func TestLoggablePublicIDMasksUser(t *testing.T) {
	if got := loggablePublicID("sip:+447785016005@ims.mnc015.mcc234.3gppnetwork.org"); got != "***@ims.mnc015.mcc234.3gppnetwork.org" {
		t.Fatalf("loggablePublicID = %q", got)
	}
	if got := loggablePublicID(""); got != "" {
		t.Fatalf("empty public id = %q", got)
	}
}

func TestSubscriptionFailureKeepsRegistration(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	client, server := net.Pipe()
	service.activateProtectedRegistrationTCP(client)
	t.Cleanup(func() { _ = server.Close() })

	service.reportSubscriptionRuntimeError(errors.New("SUBSCRIBE rejected with status 403 (Forbidden)"))
	select {
	case err := <-service.RegistrationErrors():
		t.Fatalf("SUBSCRIBE failure tore down runtime: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if service.RegState() != regRegistered {
		t.Fatalf("registration state = %s, want %s", service.RegState(), regRegistered)
	}
}

func TestRegistrationSubscriptionTimeoutIsRecorded(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	client, server := net.Pipe()
	service.activateProtectedRegistrationTCP(client)
	t.Cleanup(func() { _ = server.Close() })
	readDone := make(chan error, 1)
	go func() {
		_, err := readSIPStreamMessage(bufio.NewReader(server))
		readDone <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := service.sendSubscribeReg(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("sendSubscribeReg error = %v, want deadline exceeded", err)
	}
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
	service.mu.RLock()
	lastErr := service.subscriptionLastErr
	refreshAt := service.subscriptionRefreshAt
	service.mu.RUnlock()
	if !strings.Contains(lastErr, context.DeadlineExceeded.Error()) || refreshAt.IsZero() {
		t.Fatalf("subscription failure state = err %q refresh %s", lastErr, refreshAt)
	}
}

func TestRegistrationSubscriptionStopsWithService(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	client, server := net.Pipe()
	service.activateProtectedRegistrationTCP(client)
	t.Cleanup(func() { _ = server.Close() })
	requestRead := make(chan error, 1)
	go func() {
		_, err := readSIPStreamMessage(bufio.NewReader(server))
		requestRead <- err
	}()
	result := make(chan error, 1)
	go func() { result <- service.sendSubscribeReg(context.Background()) }()
	if err := <-requestRead; err != nil {
		t.Fatal(err)
	}
	service.StopCurrent()
	if err := <-result; err == nil || !strings.Contains(err.Error(), "service stopped") {
		t.Fatalf("sendSubscribeReg after Stop = %v", err)
	}
	if service.subscriptionInFlight.Load() {
		t.Fatal("Stop left SUBSCRIBE marked in flight")
	}
}

func TestRegistrationNotifyRepliesThenParsesReginfo(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	body := `<?xml version="1.0"?><reginfo xmlns="urn:ietf:params:xml:ns:reginfo">` +
		`<registration aor="sip:user@example"><contact id="registered-contact" state="terminated"><uri>sip:registered-contact@old.example</uri></contact></registration>` +
		`<registration aor="sip:+447840844894@o2.co.uk"><contact id="registered-contact" state="active"><uri>sip:registered-contact@new.example</uri></contact></registration></reginfo>`
	raw := registrationNotifyRequest(body)
	replied := make(chan string, 1)
	if err := service.dispatchInboundSIP(raw, func(response string) error {
		replied <- response
		return nil
	}); err != nil {
		t.Fatalf("dispatchInboundSIP: %v", err)
	}
	response := <-replied
	if !strings.HasPrefix(response, "SIP/2.0 200 OK") {
		t.Fatalf("NOTIFY response = %q", response)
	}
	waitForReginfoAOR(t, service, "sip:+447840844894@o2.co.uk")
	if service.notifyReconnectPending.Load() {
		t.Fatal("active binding did not override a stale terminated binding")
	}
}

func TestCollectReginfoStatsCountsBindingsWithoutIdentityData(t *testing.T) {
	body := []byte(`<reginfo><registration aor="sip:user@example">` +
		`<contact id="current" state="active"><uri>sip:current@new.example</uri></contact>` +
		`<contact id="stale" state="active"><uri>sip:stale@old.example</uri></contact>` +
		`<contact id="current" state="terminated"><uri>sip:current@old.example</uri></contact>` +
		`</registration></reginfo>`)
	document, err := parseReginfoXML(body)
	if err != nil {
		t.Fatal(err)
	}
	stats := collectReginfoStats(document, "current", "current")
	if stats.registrations != 1 || stats.contacts != 3 || stats.active != 2 ||
		stats.terminated != 1 || stats.currentActive != 1 || stats.currentTerminated != 1 {
		t.Fatalf("reginfo stats = %+v", stats)
	}
}

func TestDuplicateActiveRegistrationRequiresSameAORAndCurrentContact(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "duplicate in one registration",
			body: `<reginfo><registration aor="sip:user@example">` +
				`<contact id="current" state="active"><uri>sip:current@new.example</uri></contact>` +
				`<contact id="stale" state="active"><uri>sip:stale@old.example</uri></contact>` +
				`</registration></reginfo>`,
			want: true,
		},
		{
			name: "one binding for each public identity",
			body: `<reginfo><registration aor="sip:user@example">` +
				`<contact id="current" state="active"><uri>sip:current@one.example</uri></contact>` +
				`</registration><registration aor="sip:+44123@example">` +
				`<contact id="current" state="active"><uri>sip:current@two.example</uri></contact>` +
				`</registration></reginfo>`,
		},
		{
			name: "duplicates do not include current contact",
			body: `<reginfo><registration aor="sip:user@example">` +
				`<contact id="other-1" state="active"><uri>sip:other-1@one.example</uri></contact>` +
				`<contact id="other-2" state="active"><uri>sip:other-2@two.example</uri></contact>` +
				`</registration></reginfo>`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := parseReginfoXML([]byte(test.body))
			if err != nil {
				t.Fatal(err)
			}
			if got := hasDuplicateActiveRegistration(document, "current", "current"); got != test.want {
				t.Fatalf("hasDuplicateActiveRegistration = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRegistrationBindingCleanupIsGiffgaffOnlyAndOncePerIdentity(t *testing.T) {
	document, err := parseReginfoXML([]byte(`<reginfo><registration aor="sip:user@example">` +
		`<contact id="current" state="active"><uri>sip:current@new.example</uri></contact>` +
		`<contact id="stale" state="active"><uri>sip:stale@old.example</uri></contact>` +
		`</registration></reginfo>`))
	if err != nil {
		t.Fatal(err)
	}
	newService := func(preset, device string) *Service {
		return &Service{cfg: &IMSConfig{CarrierPresetID: preset, DeviceID: device, IMPU: "sip:user@example"},
			regSession: &registerSession{contactUser: "current", publicID: "sip:user@example", authHeader: "Digest auth"}}
	}
	if newService("CTEUK_23433", "cte").requestRegistrationBindingCleanup(document) {
		t.Fatal("non-giffgaff carrier requested wildcard cleanup")
	}
	service := newService(giffgaffCarrierPresetID, "giffgaff-once")
	if !service.requestRegistrationBindingCleanup(document) || !service.bindingCleanupPending.Load() {
		t.Fatal("giffgaff duplicate binding did not request cleanup")
	}
	if service.requestRegistrationBindingCleanup(document) {
		t.Fatal("giffgaff duplicate binding requested cleanup more than once")
	}
}

func TestRegistrationNotifyDeduplicatesReRegistration(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	body := `<reginfo><registration aor="sip:+447840844894@o2.co.uk">` +
		`<contact id="registered-contact" state="terminated"><uri>sip:registered-contact@old.example</uri></contact>` +
		`</registration></reginfo>`
	raw := registrationNotifyRequest(body)
	service.handleRegistrationNotification(raw)
	service.handleRegistrationNotification(raw)
	if !service.notifyReconnectPending.Load() {
		t.Fatal("terminated binding did not schedule re-registration")
	}
	service.StopCurrent()
	if service.notifyReconnectPending.Load() {
		t.Fatal("Stop did not clear the pending reginfo re-registration")
	}
}

func TestMalformedRegistrationNotifyIsAcknowledgedWithoutMutation(t *testing.T) {
	service := newProtectedKeepaliveTestService(t)
	raw := registrationNotifyRequest(`<reginfo><registration`)
	replied := make(chan string, 1)
	if err := service.dispatchInboundSIP(raw, func(response string) error {
		replied <- response
		return nil
	}); err != nil {
		t.Fatalf("dispatchInboundSIP: %v", err)
	}
	if response := <-replied; !strings.HasPrefix(response, "SIP/2.0 200 OK") {
		t.Fatalf("NOTIFY response = %q", response)
	}
	time.Sleep(10 * time.Millisecond)
	service.mu.RLock()
	aor := service.reginfoAOR
	service.mu.RUnlock()
	if aor != "" || service.notifyReconnectPending.Load() {
		t.Fatalf("malformed reginfo mutated state: aor=%q pending=%v", aor, service.notifyReconnectPending.Load())
	}
}

func TestReginfoAORPrefersTelephoneIdentityAndSummaryIsBounded(t *testing.T) {
	body := []byte(`<reginfo><registration aor="sip:user@example">` +
		`<contact id="1" state="active"><uri>sip:1@example</uri></contact>` +
		`<contact id="2" state="active"><uri>sip:2@example</uri></contact>` +
		`<contact id="3" state="active"><uri>sip:3@example</uri></contact>` +
		`<contact id="4" state="active"><uri>sip:4@example</uri></contact>` +
		`<contact id="5" state="active"><uri>sip:5@example</uri></contact>` +
		`<contact id="6" state="active"><uri>sip:6@example</uri></contact>` +
		`<contact id="7" state="active"><uri>sip:7@example</uri></contact>` +
		`</registration><registration aor="sip:+447840844894@o2.co.uk"/></reginfo>`)
	if got := extractReginfoAOR(body); got != "sip:+447840844894@o2.co.uk" {
		t.Fatalf("extractReginfoAOR = %q", got)
	}
	summary := summarizeReginfoXML(body)
	if strings.Contains(summary, "id=7,") || strings.Count(summary, "id=") != reginfoSummaryLimit {
		t.Fatalf("reginfo summary is not bounded to %d contacts: %q", reginfoSummaryLimit, summary)
	}
}

func TestSubscriptionRefreshDelayMatchesRecoveredClient(t *testing.T) {
	if got := subscriptionRefreshDelay(time.Hour); got != 59*time.Minute {
		t.Fatalf("hour subscription refresh delay = %s", got)
	}
	if got := subscriptionRefreshDelay(time.Minute); got != 0 {
		t.Fatalf("one-minute subscription refresh delay = %s, want immediate", got)
	}
}

func registrationNotifyRequest(body string) string {
	return "NOTIFY sip:user@example SIP/2.0\r\n" +
		"Via: SIP/2.0/TCP 192.0.2.1:6060;branch=z9hG4bK-notify\r\n" +
		"From: <sip:server@example>;tag=server\r\n" +
		"To: <sip:user@example>;tag=client\r\n" +
		"Call-ID: notify-call\r\nCSeq: 1 NOTIFY\r\n" +
		"Event: reg;id=registration\r\nContent-Type: application/reginfo+xml;charset=UTF-8\r\n" +
		"Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
}

func waitForReginfoAOR(t *testing.T, service *Service, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		service.mu.RLock()
		got := service.reginfoAOR
		service.mu.RUnlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("reginfo AOR was not updated to %q", want)
}
