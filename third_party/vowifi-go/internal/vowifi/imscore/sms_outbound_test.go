package imscore

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
)

type captureDeliveryStore struct {
	mu          sync.Mutex
	created     *DeliveryStatus
	partStates  []string
	sipResults  []capturedSIPResult
	finalState  string
	finalError  string
	createError error
	parts       map[string]capturedDeliveryPart
	reportCalls int
}

type capturedSIPResult struct {
	code  int
	state string
	err   string
}

type capturedDeliveryPart struct {
	messageID string
	partNo    int
	callID    string
	rpMR      int
}

func (s *captureDeliveryStore) CreateSMSDelivery(messageID, imsi, deviceID, peer, content string, partsTotal int, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createError != nil {
		return s.createError
	}
	s.created = &DeliveryStatus{
		MessageID: messageID, IMSI: imsi, DeviceID: deviceID,
		Peer: peer, Content: content, PartsTotal: partsTotal, State: smsDeliveryStatePending,
	}
	return nil
}

func (s *captureDeliveryStore) UpsertSMSDeliveryPart(messageID string, partNo int, callID string, rpMR int, state string, _ time.Time) error {
	s.mu.Lock()
	if s.parts == nil {
		s.parts = make(map[string]capturedDeliveryPart)
	}
	s.parts[callID] = capturedDeliveryPart{messageID: messageID, partNo: partNo, callID: callID, rpMR: rpMR}
	s.partStates = append(s.partStates, state)
	s.mu.Unlock()
	return nil
}

func (s *captureDeliveryStore) MarkSMSDeliveryPartSIPResult(
	_ string,
	_, sipCode int,
	state, errText string,
	_ time.Time,
) error {
	s.mu.Lock()
	s.sipResults = append(s.sipResults, capturedSIPResult{code: sipCode, state: state, err: errText})
	s.mu.Unlock()
	return nil
}

func (s *captureDeliveryStore) MarkSMSDeliveryPartReport(inReplyTo, callID, _ string, rpMR int, state string, _ int, _ int, _ string, _ time.Time) (DeliveryPartMatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reportCalls++
	for _, key := range []string{inReplyTo, callID} {
		if part, ok := s.parts[key]; ok {
			return DeliveryPartMatch{MessageID: part.messageID, PartNo: part.partNo, State: state, Matched: true}, nil
		}
	}
	for _, part := range s.parts {
		if part.rpMR == rpMR {
			return DeliveryPartMatch{MessageID: part.messageID, PartNo: part.partNo, State: state, Matched: true}, nil
		}
	}
	return DeliveryPartMatch{}, errors.New("delivery part not found")
}

func (s *captureDeliveryStore) RecomputeSMSDelivery(messageID string, _ time.Time) error {
	s.mu.Lock()
	if s.created != nil && s.created.MessageID == messageID {
		s.created.State = smsDeliveryStateAcked
	}
	s.mu.Unlock()
	return nil
}

func (s *captureDeliveryStore) UpdateSMSDeliveryState(_ string, state, lastError string, _ int, _ time.Time) error {
	s.mu.Lock()
	s.finalState, s.finalError = state, lastError
	s.mu.Unlock()
	return nil
}

func (s *captureDeliveryStore) GetSMSDeliveryStatus(string) (*DeliveryStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.created == nil {
		return nil, errors.New("delivery not found")
	}
	status := *s.created
	return &status, nil
}

func TestSendOutboundSMSWaitsForSIPSuccess(t *testing.T) {
	service, subscriber, store := newOutboundSMSTestService(t)
	requests := make(chan string, 1)
	service.transport.SetSendFn(func(request string) error {
		requests <- request
		return nil
	})

	results := make(chan SendOutcome, 1)
	errors := make(chan error, 1)
	go func() {
		outcome, err := service.SendSMSWithResult(context.Background(), "+44 7700 900123", "hello")
		results <- outcome
		errors <- err
	}()
	request := waitForOutboundSMSControl(t, requests)
	assertOutboundSMSRequest(t, request, "+447700900123", "+447802002606")

	select {
	case event := <-subscriber.events:
		accepted, ok := event.(events.EventSMSSendAccepted)
		if !ok || accepted.MessageID == "" || accepted.TargetURI != "+447700900123" ||
			accepted.PartsTotal != 1 || accepted.AcceptedAt.IsZero() || accepted.Time != accepted.AcceptedAt ||
			accepted.ExpiresHint != smsSendAcceptedExpiresHint {
			t.Fatalf("accepted event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("SMS acceptance event was not published")
	}
	select {
	case event := <-subscriber.events:
		t.Fatalf("SMS success published before final response: %#v", event)
	case <-results:
		t.Fatal("SMS send returned before final response")
	case <-time.After(20 * time.Millisecond):
	}
	service.transport.DeliverResponse(registerResponseForRequest(request, 200, nil))

	select {
	case err := <-errors:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("SMS send did not finish after SIP 200")
	}
	outcome := <-results
	if outcome.DeliveryState != smsDeliveryStatePending || outcome.PartsTotal != 1 || outcome.MessageID == "" {
		t.Fatalf("outcome = %+v", outcome)
	}
	assertIMSEventTypes(t, subscriber, "SMSDeliveryUpdated", "LogNotify")
	event := <-subscriber.events
	sent, ok := event.(*events.EventSMSSent)
	if !ok || sent.TargetURI != "+447700900123" || sent.Content != "hello" || sent.TotalParts != 1 {
		t.Fatalf("sent event = %#v", event)
	}
	if store.created == nil || store.created.Peer != "+447700900123" || len(store.partStates) != 1 || store.partStates[0] != smsDeliveryStatePending {
		t.Fatalf("delivery store = %+v, parts = %v", store.created, store.partStates)
	}
	if len(store.sipResults) != 1 || store.sipResults[0].code != 200 || store.sipResults[0].state != smsDeliveryStatePending {
		t.Fatalf("SIP results = %+v", store.sipResults)
	}
	if store.reportCalls != 0 || store.created.State != smsDeliveryStatePending {
		t.Fatalf("SIP acceptance fabricated a delivery report: calls=%d status=%+v", store.reportCalls, store.created)
	}
}

func assertIMSEventTypes(t *testing.T, subscriber *captureIMSEventSubscriber, types ...string) {
	t.Helper()
	for _, want := range types {
		select {
		case event := <-subscriber.events:
			if event.Type() != want {
				t.Fatalf("event = %#v, want %s", event, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing %s event", want)
		}
	}
}

func TestSendOutboundSMSRejectsNon2xxWithoutSuccessEvent(t *testing.T) {
	service, subscriber, store := newOutboundSMSTestService(t)
	service.transport.SetSendFn(func(request string) error {
		response := registerResponseForRequest(request, 503, nil)
		response.Reason = "Service Unavailable"
		service.transport.DeliverResponse(response)
		return nil
	})
	outcome, err := service.SendSMSWithResult(context.Background(), "+447700900123", "hello")
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("send error = %v", err)
	}
	if outcome.MessageID == "" || outcome.DeliveryState != smsDeliveryStateFailed {
		t.Fatalf("failed send outcome = %+v", outcome)
	}
	assertAcceptedEvent(t, subscriber, outcome.MessageID)
	select {
	case event := <-subscriber.events:
		t.Fatalf("failed SMS published success event: %#v", event)
	default:
	}
	if store.finalState != smsDeliveryStateFailed || !strings.Contains(store.finalError, "503") {
		t.Fatalf("failure state = %q, error = %q", store.finalState, store.finalError)
	}
	if len(store.sipResults) != 1 || store.sipResults[0].code != 503 || store.sipResults[0].state != smsDeliveryStateFailed {
		t.Fatalf("SIP results = %+v", store.sipResults)
	}
	want := []string{smsDeliveryStatePending, smsDeliveryStateFailed}
	if strings.Join(store.partStates, ",") != strings.Join(want, ",") {
		t.Fatalf("part states = %v", store.partStates)
	}
}

func TestSendOutboundSMSSurfacesCallerDeadline(t *testing.T) {
	service, subscriber, store := newOutboundSMSTestService(t)
	service.transport.SetSendFn(func(string) error { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := service.SendSMSWithResult(ctx, "+447700900123", "hello")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("send error = %v", err)
	}
	if !strings.Contains(err.Error(), "caller deadline exceeded") {
		t.Fatalf("send error = %v", err)
	}
	assertAcceptedEvent(t, subscriber, "")
	select {
	case event := <-subscriber.events:
		t.Fatalf("timed-out SMS published success event: %#v", event)
	default:
	}
	if store.finalState != smsDeliveryStateFailed {
		t.Fatalf("failure state = %q", store.finalState)
	}
}

func assertAcceptedEvent(t *testing.T, subscriber *captureIMSEventSubscriber, messageID string) {
	t.Helper()
	select {
	case event := <-subscriber.events:
		accepted, ok := event.(events.EventSMSSendAccepted)
		if !ok || (messageID != "" && accepted.MessageID != messageID) {
			t.Fatalf("accepted event = %#v, message ID %q", event, messageID)
		}
	case <-time.After(time.Second):
		t.Fatal("missing SMS acceptance event")
	}
}

func TestSendOutboundSMSSurfacesInternalFinalResponseTimeout(t *testing.T) {
	service, _, store := newOutboundSMSTestService(t)
	service.smsTransactionTimeout = 20 * time.Millisecond
	service.smsReportTimeout = 30 * time.Millisecond
	service.transport.SetSendFn(func(string) error { return nil })

	_, err := service.SendSMSWithResult(context.Background(), "+447700900123", "hello")
	if !errors.Is(err, context.DeadlineExceeded) ||
		!strings.Contains(err.Error(), "final response timeout after 20ms") ||
		!strings.Contains(err.Error(), "SMS delivery report timeout after 30ms") {
		t.Fatalf("send error = %v", err)
	}
	if len(store.sipResults) != 1 || store.sipResults[0].code != 0 || store.sipResults[0].state != smsDeliveryStateFailed {
		t.Fatalf("SIP results = %+v", store.sipResults)
	}
}

func TestTCPSubmitProbeTimeoutWaitsForLateRPReport(t *testing.T) {
	service, subscriber, store := newOutboundSMSTestService(t)
	service.cfg.Transport = "tcp"
	service.smsTransactionTimeout = 20 * time.Millisecond
	service.smsReportTimeout = 250 * time.Millisecond
	requests := make(chan string, 1)
	service.transport.SetSendFn(func(request string) error {
		requests <- request
		return nil
	})

	resultCh := make(chan SendOutcome, 1)
	errCh := make(chan error, 1)
	go func() {
		outcome, err := service.SendSMSWithResult(context.Background(), "+447700900123", "late report")
		resultCh <- outcome
		errCh <- err
	}()
	request := waitForOutboundSMSControl(t, requests)
	assertAcceptedEvent(t, subscriber, "")
	time.Sleep(2 * service.smsTransactionTimeout)
	callID := rawSIPHeaderValue(request, "Call-ID")
	part := store.parts[callID]
	report := deliveryReportRequest([]byte{0x03, byte(part.rpMR)}, callID)
	if err := service.dispatchInboundSIP(report, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if outcome := <-resultCh; outcome.DeliveryState != smsDeliveryStateAcked {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestTCPSubmitProbeTimeoutAcceptsLateSIPFinal(t *testing.T) {
	service, _, store := newOutboundSMSTestService(t)
	service.smsTransactionTimeout = 20 * time.Millisecond
	service.smsReportTimeout = 250 * time.Millisecond
	requests := make(chan string, 1)
	service.transport.SetSendFn(func(request string) error {
		requests <- request
		return nil
	})

	resultCh := make(chan SendOutcome, 1)
	errCh := make(chan error, 1)
	go func() {
		outcome, err := service.SendSMSWithResult(context.Background(), "+447700900123", "late final")
		resultCh <- outcome
		errCh <- err
	}()
	request := waitForOutboundSMSControl(t, requests)
	time.Sleep(2 * service.smsTransactionTimeout)
	assertTransactionCount(t, service.transport, 1)
	service.transport.DeliverResponse(registerResponseForRequest(request, 202, nil))

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if outcome := <-resultCh; outcome.DeliveryState != smsDeliveryStatePending {
		t.Fatalf("outcome = %+v", outcome)
	}
	waitTransactionCount(t, service.transport, 0)
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.sipResults) != 1 || store.sipResults[0].code != 202 ||
		store.sipResults[0].state != smsDeliveryStatePending {
		t.Fatalf("SIP results = %+v", store.sipResults)
	}
}

func TestTCPSubmitProbeTimeoutRejectsLateSIPFinal(t *testing.T) {
	service, _, store := newOutboundSMSTestService(t)
	service.smsTransactionTimeout = 20 * time.Millisecond
	service.smsReportTimeout = 250 * time.Millisecond
	requests := make(chan string, 1)
	service.transport.SetSendFn(func(request string) error {
		requests <- request
		return nil
	})

	errCh := make(chan error, 1)
	go func() {
		_, err := service.SendSMSWithResult(context.Background(), "+447700900123", "late reject")
		errCh <- err
	}()
	request := waitForOutboundSMSControl(t, requests)
	time.Sleep(2 * service.smsTransactionTimeout)
	assertTransactionCount(t, service.transport, 1)
	response := registerResponseForRequest(request, 503, nil)
	response.Reason = "Service Unavailable"
	service.transport.DeliverResponse(response)

	if err := <-errCh; err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("send error = %v", err)
	}
	waitTransactionCount(t, service.transport, 0)
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.sipResults) != 1 || store.sipResults[0].code != 503 ||
		store.sipResults[0].state != smsDeliveryStateFailed || store.finalState != smsDeliveryStateFailed {
		t.Fatalf("SIP results = %+v, final state = %q", store.sipResults, store.finalState)
	}
}

func TestTCPSubmitReportWaitHonorsCallerDeadline(t *testing.T) {
	service, _, store := newOutboundSMSTestService(t)
	service.cfg.Transport = "tcp"
	service.smsTransactionTimeout = 20 * time.Millisecond
	service.smsReportTimeout = time.Second
	service.transport.SetSendFn(func(string) error { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	startedAt := time.Now()
	_, err := service.SendSMSWithResult(ctx, "+447700900123", "caller deadline")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("send error = %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed >= service.smsReportTimeout {
		t.Fatalf("send ignored caller deadline: elapsed=%s", elapsed)
	}
	if store.finalState != smsDeliveryStateFailed {
		t.Fatalf("failure state = %q", store.finalState)
	}
}

func TestUDPSubmitProbeTimeoutReturnsPendingAndRetainsRPReport(t *testing.T) {
	service, subscriber, store := newOutboundSMSTestService(t)
	service.cfg.Transport = "udp"
	service.smsTransactionTimeout = 20 * time.Millisecond
	service.smsReportTimeout = 250 * time.Millisecond
	requests := make(chan string, 1)
	service.transport.SetSendFn(func(request string) error {
		requests <- request
		return nil
	})

	outcome, err := service.SendSMSWithResult(context.Background(), "+447700900123", "soft timeout")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.DeliveryState != smsDeliveryStatePending {
		t.Fatalf("outcome = %+v", outcome)
	}
	request := <-requests
	assertAcceptedEvent(t, subscriber, "")
	assertIMSEventTypes(t, subscriber, "SMSDeliveryUpdated", "LogNotify", "SMSSent")
	if len(store.sipResults) != 1 || store.sipResults[0].state != smsDeliveryStatePending {
		t.Fatalf("SIP results = %+v", store.sipResults)
	}
	callID := rawSIPHeaderValue(request, "Call-ID")
	part := store.parts[callID]
	if part.callID == "" {
		t.Fatalf("pending part for %q was not persisted", callID)
	}
	report := deliveryReportRequest([]byte{0x03, byte(part.rpMR)}, callID)
	if err := service.dispatchInboundSIP(report, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	assertIMSEventTypes(t, subscriber, "SMSDeliveryUpdated", "SMSDeliveryCompleted")
}

func TestSendOutboundSMSSurfacesCallerCancellation(t *testing.T) {
	service, _, _ := newOutboundSMSTestService(t)
	service.transport.SetSendFn(func(string) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.SendSMSWithResult(ctx, "+447700900123", "hello")
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "canceled by caller") {
		t.Fatalf("send error = %v", err)
	}
}

func TestSendOutboundSMSFailsWhenRPMRRandomnessFails(t *testing.T) {
	service, _, _ := newOutboundSMSTestService(t)
	service.smsRandom = iotest.ErrReader(errors.New("entropy unavailable"))

	_, err := service.SendSMSWithResult(context.Background(), "+447700900123", "hello")
	if err == nil || !strings.Contains(err.Error(), "allocate RP-MR") ||
		!strings.Contains(err.Error(), "entropy unavailable") {
		t.Fatalf("send error = %v", err)
	}
}

func newOutboundSMSTestService(t *testing.T) (*Service, *captureIMSEventSubscriber, *captureDeliveryStore) {
	t.Helper()
	bus := NewEventBus()
	subscriber := &captureIMSEventSubscriber{events: make(chan events.Event, 4)}
	bus.Subscribe(subscriber)
	store := &captureDeliveryStore{}
	service, err := New(&IMSConfig{
		DeviceID: "wwan0", IMSI: "234102356143376", IMPI: "234102356143376@ims.example",
		IMPU: "sip:234102356143376@ims.example", Domain: "ims.example", SMSC: "+447802002606",
		LocalIP: net.IPv4(10, 0, 0, 2), LocalPort: 5060, Transport: "tcp", EventBus: bus, DeliveryStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.regState = regRegistered
	service.externalTransport = true
	service.smsReceiverReady = true
	service.regSession = &registerSession{
		contactUser: "registered-contact", cseq: 3,
		publicID:     "sip:+447840844894@o2.co.uk",
		serviceRoute: "<sip:pcscf.ims.example;lr>",
		security:     &securityAgreement{verifyHeader: "ipsec-3gpp;alg=hmac-sha-1-96"},
	}
	service.mu.Unlock()
	t.Cleanup(service.Stop)
	return service, subscriber, store
}

func assertOutboundSMSRequest(t *testing.T, request, recipient, smsc string) {
	t.Helper()
	if !strings.HasPrefix(request, "MESSAGE sip:"+recipient+"@ims.example;user=phone SIP/2.0") {
		t.Fatalf("request URI = %q", strings.SplitN(request, "\r\n", 2)[0])
	}
	if got := rawSIPHeaderValue(request, "Content-Type"); got != imsSMSContentType {
		t.Fatalf("Content-Type = %q", got)
	}
	wantHeaders := map[string]string{
		"From":                 "<sip:+447840844894@o2.co.uk>",
		"Contact":              "<sip:registered-contact@10.0.0.2:5060>",
		"Route":                "<sip:pcscf.ims.example;lr>",
		"P-Preferred-Identity": "<sip:+447840844894@o2.co.uk>",
		"Security-Verify":      "ipsec-3gpp;alg=hmac-sha-1-96",
		"Supported":            smsSupportedHeader + ", sec-agree",
		"Request-Disposition":  "no-fork",
	}
	for name, want := range wantHeaders {
		got := rawSIPHeaderValue(request, name)
		if name == "From" {
			got = strings.SplitN(got, ";tag=", 2)[0]
		}
		if got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	if got := rawSIPHeaderValue(request, "CSeq"); got != "6 MESSAGE" {
		t.Fatalf("CSeq = %q", got)
	}
	if strings.Contains(request, "\r\nRequire: sec-agree\r\n") || strings.Contains(request, "\r\nProxy-Require: sec-agree\r\n") {
		t.Fatalf("MESSAGE unexpectedly requires sec-agree: %q", request)
	}
	body, err := rawSIPBody(request)
	if err != nil {
		t.Fatal(err)
	}
	info := smscodec.ClassifyRPDU(body)
	if info.Kind != smscodec.RPDUKindData || info.RawType != 0x00 {
		t.Fatalf("RP-DATA = %+v", info)
	}
	_, originator, destination, submit, err := smscodec.ParseRPDataWithAddresses(body)
	if err != nil {
		t.Fatal(err)
	}
	if originator != "" || destination != smsc || len(submit) == 0 || submit[0]&0x03 != 0x01 {
		t.Fatalf("RP addresses originator=%q destination=%q TPDU=%x", originator, destination, submit)
	}
}

func TestBuildSMSMESSAGEAllocatesUniqueCSeqAcrossConcurrentRequests(t *testing.T) {
	service, _, _ := newOutboundSMSTestService(t)
	const requests = 32
	results := make(chan string, requests)
	errorsCh := make(chan error, requests)
	var wg sync.WaitGroup
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request, err := service.buildSMSMESSAGE("sip:+447700900123@ims.example;user=phone", []byte{0x00})
			if err != nil {
				errorsCh <- err
				return
			}
			results <- rawSIPHeaderValue(request, "CSeq")
		}()
	}
	wg.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		t.Fatal(err)
	}
	seen := make(map[string]bool, requests)
	for cseq := range results {
		if seen[cseq] {
			t.Fatalf("duplicate CSeq %q", cseq)
		}
		seen[cseq] = true
	}
	if len(seen) != requests {
		t.Fatalf("CSeq count = %d, want %d", len(seen), requests)
	}
}

func TestBuildSMSMESSAGERequiresNegotiatedRegistrationIdentity(t *testing.T) {
	service, _, _ := newOutboundSMSTestService(t)
	service.mu.Lock()
	service.regSession.publicID = ""
	service.mu.Unlock()

	_, err := service.buildSMSMESSAGE("sip:+447700900123@ims.example;user=phone", []byte{0x00})
	if err == nil || !strings.Contains(err.Error(), "registered public identity is unavailable") {
		t.Fatalf("build error = %v", err)
	}
}
