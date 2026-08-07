package imscore

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
)

type captureDeliveryStore struct {
	mu          sync.Mutex
	created     *DeliveryStatus
	partStates  []string
	finalState  string
	finalError  string
	createError error
}

func (s *captureDeliveryStore) CreateSMSDelivery(messageID, imsi, deviceID, peer, content string, partsTotal int, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createError != nil {
		return s.createError
	}
	s.created = &DeliveryStatus{
		MessageID: messageID, IMSI: imsi, DeviceID: deviceID,
		Peer: peer, Content: content, PartsTotal: partsTotal,
	}
	return nil
}

func (s *captureDeliveryStore) UpsertSMSDeliveryPart(_ string, _ int, _ string, _ int, state string, _ time.Time) error {
	s.mu.Lock()
	s.partStates = append(s.partStates, state)
	s.mu.Unlock()
	return nil
}

func (s *captureDeliveryStore) MarkSMSDeliveryPartReport(string, string, string, int, string, int, int, string, time.Time) (DeliveryPartMatch, error) {
	return DeliveryPartMatch{}, nil
}

func (s *captureDeliveryStore) RecomputeSMSDelivery(string, time.Time) error { return nil }

func (s *captureDeliveryStore) UpdateSMSDeliveryState(_ string, state, lastError string, _ int, _ time.Time) error {
	s.mu.Lock()
	s.finalState, s.finalError = state, lastError
	s.mu.Unlock()
	return nil
}

func (s *captureDeliveryStore) GetSMSDeliveryStatus(string) (*DeliveryStatus, error) {
	return nil, errors.New("not implemented")
}

func TestSendOutboundSMSWaitsForSIPSuccess(t *testing.T) {
	service, subscriber, store := newOutboundSMSTestService(t)
	requests := make(chan string, 1)
	service.transport.SetSendFn(func(request string) error {
		requests <- request
		return nil
	})

	results := make(chan *SMSSendOutcome, 1)
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
	if outcome.State != smsDeliveryStatePending || outcome.PartsTotal != 1 || outcome.MessageID == "" {
		t.Fatalf("outcome = %+v", outcome)
	}
	event := <-subscriber.events
	sent, ok := event.(*events.EventSMSSent)
	if !ok || sent.TargetURI != "+447700900123" || sent.Content != "hello" || sent.TotalParts != 1 {
		t.Fatalf("sent event = %#v", event)
	}
	if store.created == nil || store.created.Peer != "+447700900123" || len(store.partStates) != 1 || store.partStates[0] != smsDeliveryStatePending {
		t.Fatalf("delivery store = %+v, parts = %v", store.created, store.partStates)
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
	_, err := service.SendSMSWithResult(context.Background(), "+447700900123", "hello")
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("send error = %v", err)
	}
	select {
	case event := <-subscriber.events:
		t.Fatalf("failed SMS published success event: %#v", event)
	default:
	}
	if store.finalState != smsDeliveryStateFailed || !strings.Contains(store.finalError, "503") {
		t.Fatalf("failure state = %q, error = %q", store.finalState, store.finalError)
	}
	want := []string{smsDeliveryStatePending, smsDeliveryStateFailed}
	if strings.Join(store.partStates, ",") != strings.Join(want, ",") {
		t.Fatalf("part states = %v", store.partStates)
	}
}

func TestSendOutboundSMSSurfacesTransactionTimeout(t *testing.T) {
	service, subscriber, store := newOutboundSMSTestService(t)
	service.transport.SetSendFn(func(string) error { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := service.SendSMSWithResult(ctx, "+447700900123", "hello")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("send error = %v", err)
	}
	select {
	case event := <-subscriber.events:
		t.Fatalf("timed-out SMS published success event: %#v", event)
	default:
	}
	if store.finalState != smsDeliveryStateFailed {
		t.Fatalf("failure state = %q", store.finalState)
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
		IMPU: []string{"sip:234102356143376@ims.example"}, Domain: "ims.example", SMSC: "+447802002606",
		LocalIP: net.IPv4(10, 0, 0, 2), LocalPort: 5060, Transport: "tcp", EventBus: bus, DeliveryStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.regState = regRegistered
	service.smsReceiverReady = true
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
