package imscore

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/warthog618/sms/encoding/tpdu"
)

func TestInboundRPAckCompletesSMSDelivery(t *testing.T) {
	service, subscriber, store, outbound := newDeliveryReportTestService(t)
	outcome := sendDeliveryTestSMS(t, service, subscriber, outbound, "hello")
	part := store.part(outcome.MessageID, 1)
	response := dispatchDeliveryReport(t, service, deliveryReportRequest([]byte{0x03, byte(part.rpMR)}, part.callID))
	if !strings.HasPrefix(response, "SIP/2.0 200") {
		t.Fatalf("SIP response = %q", response)
	}
	assertDeliveryStatus(t, store, outcome.MessageID, smsDeliveryStateAcked, smsDeliveryStateAcked)
	assertDeliveryEvents(t, subscriber, outcome.MessageID, "SMSDeliveryUpdated", "SMSDeliveryCompleted")
}

func TestInboundRPErrorFailsSMSDelivery(t *testing.T) {
	service, subscriber, store, outbound := newDeliveryReportTestService(t)
	outcome := sendDeliveryTestSMS(t, service, subscriber, outbound, "hello")
	part := store.part(outcome.MessageID, 1)
	dispatchDeliveryReport(t, service, deliveryReportRequest([]byte{0x05, byte(part.rpMR), 0x01, 0x29, 0x00}, part.callID))
	assertDeliveryStatus(t, store, outcome.MessageID, smsDeliveryStateFailed, smsDeliveryStateFailed)
	assertDeliveryEvents(t, subscriber, outcome.MessageID, "SMSDeliveryUpdated", "SMSDeliveryFailed")
}

func TestInboundTPStatusReportMatchesTPMRAndSendsRPAck(t *testing.T) {
	service, subscriber, store, outbound := newDeliveryReportTestService(t)
	outcome := sendDeliveryTestSMS(t, service, subscriber, outbound, "hello")
	part := store.part(outcome.MessageID, 1)
	tpStatus := statusReportTPDU(t, byte(part.rpMR), 0x00)
	rpMR := byte(0x71)
	dispatchDeliveryReport(t, service, deliveryReportRequest(networkRPData(t, rpMR, tpStatus), part.callID))
	assertDeliveryStatus(t, store, outcome.MessageID, smsDeliveryStateAcked, smsDeliveryStateAcked)
	assertDeliveryEvents(t, subscriber, outcome.MessageID, "SMSDeliveryUpdated", "SMSDeliveryCompleted")

	request := waitForOutboundSMSControl(t, outbound)
	body, err := rawSIPBody(request)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(smscodec.BuildRPAck(rpMR)) {
		t.Fatalf("RP-ACK = %x", body)
	}
}

func TestTPStatusReportStateMapping(t *testing.T) {
	tests := []struct {
		status byte
		state  string
	}{
		{status: 0x00, state: smsDeliveryStateAcked},
		{status: 0x20, state: smsDeliveryStatePending},
		{status: 0x40, state: smsDeliveryStateFailed},
	}
	for _, test := range tests {
		report, err := parseTPStatusReport(statusReportTPDU(t, 7, test.status))
		if err != nil {
			t.Fatal(err)
		}
		if report.state != test.state || report.reference != 7 || report.rpCause != int(test.status) {
			t.Fatalf("status 0x%02x report = %+v", test.status, report)
		}
	}
}

func TestSMSDeliveryReportTimeoutFailsPendingPart(t *testing.T) {
	service, subscriber, store, outbound := newDeliveryReportTestService(t)
	service.smsReportTimeout = 20 * time.Millisecond
	outcome := sendDeliveryTestSMS(t, service, subscriber, outbound, "hello")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, _ := store.GetSMSDeliveryStatus(outcome.MessageID)
		if status.State == smsDeliveryStateFailed {
			assertDeliveryStatus(t, store, outcome.MessageID, smsDeliveryStateFailed, smsDeliveryPartStateTimeout)
			assertDeliveryEvents(t, subscriber, outcome.MessageID, "SMSDeliveryUpdated", "SMSDeliveryFailed")
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pending SMS did not expire")
}

func TestUnmatchedDeliveryReportReturnsErrorAfterSIPResponse(t *testing.T) {
	service, _, _, _ := newDeliveryReportTestService(t)
	var response string
	err := service.dispatchInboundSIP(deliveryReportRequest([]byte{0x03, 0x42}, "missing"), func(value string) error {
		response = value
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("dispatch error = %v", err)
	}
	if !strings.HasPrefix(response, "SIP/2.0 200") {
		t.Fatalf("SIP response = %q", response)
	}
}

func newDeliveryReportTestService(t *testing.T) (*Service, *captureIMSEventSubscriber, *memoryDeliveryStore, <-chan string) {
	t.Helper()
	bus := NewEventBus()
	subscriber := &captureIMSEventSubscriber{events: make(chan events.Event, 16)}
	bus.Subscribe(subscriber)
	store := newMemoryDeliveryStore()
	service, err := New(&IMSConfig{
		DeviceID: "wwan0", IMSI: "234102356143376", IMPI: "234102356143376@ims.example",
		IMPU: []string{"sip:234102356143376@ims.example"}, Domain: "ims.example", SMSC: "+447802002606",
		LocalIP: net.IPv4(10, 0, 0, 2), LocalPort: 5060, Transport: "tcp", EventBus: bus, DeliveryStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.regState, service.smsReceiverReady = regRegistered, true
	service.externalTransport = true
	service.regSession = &registerSession{
		contactUser: "registered-contact", cseq: 3,
		publicID: "sip:+447840844894@o2.co.uk", serviceRoute: "<sip:pcscf.ims.example;lr>",
		security: &securityAgreement{verifyHeader: "ipsec-3gpp;alg=hmac-sha-1-96"},
	}
	service.mu.Unlock()
	outbound := make(chan string, 16)
	service.transport.SetSendFn(func(request string) error {
		outbound <- request
		service.transport.DeliverResponse(registerResponseForRequest(request, 200, nil))
		return nil
	})
	t.Cleanup(service.Stop)
	return service, subscriber, store, outbound
}

func sendDeliveryTestSMS(t *testing.T, service *Service, subscriber *captureIMSEventSubscriber, outbound <-chan string, text string) *SMSSendOutcome {
	t.Helper()
	outcome, err := service.SendSMSWithResult(context.Background(), "+447700900123", text)
	if err != nil {
		t.Fatal(err)
	}
	for range outcome.PartsTotal {
		_ = waitForOutboundSMSControl(t, outbound)
	}
	if event := <-subscriber.events; event.Type() != "SMSSent" {
		t.Fatalf("first event = %s", event.Type())
	}
	return outcome
}

func dispatchDeliveryReport(t *testing.T, service *Service, request string) string {
	t.Helper()
	var response string
	if err := service.dispatchInboundSIP(request, func(value string) error {
		response = value
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return response
}

func deliveryReportRequest(body []byte, inReplyTo string) string {
	return fmt.Sprintf("MESSAGE sip:user@ims.example SIP/2.0\r\n"+
		"Via: SIP/2.0/TCP 10.0.0.1:5060;branch=z9hG4bK-report\r\n"+
		"From: <sip:+447802002606@ims.example>;tag=remote\r\n"+
		"To: <sip:user@ims.example>\r\nCall-ID: report-call\r\n"+
		"In-Reply-To: %s\r\nCSeq: 1 MESSAGE\r\nContent-Type: %s\r\n"+
		"Content-Length: %d\r\n\r\n%s", inReplyTo, imsSMSContentType, len(body), body)
}

func networkRPData(t *testing.T, rpMR byte, payload []byte) []byte {
	t.Helper()
	originator, err := smscodec.EncodeAddress("+447802002606")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte{0x01, rpMR}
	body = append(body, originator...)
	body = append(body, 0x00, byte(len(payload)))
	return append(body, payload...)
}

func statusReportTPDU(t *testing.T, messageReference, status byte) []byte {
	t.Helper()
	now := time.Now()
	report := &tpdu.TPDU{
		Direction: tpdu.MT, FirstOctet: 0x02, MR: messageReference,
		RA:   tpdu.NewAddress(tpdu.FromNumber("+447700900123")),
		SCTS: tpdu.Timestamp{Time: now}, DT: tpdu.Timestamp{Time: now}, ST: status,
	}
	raw, err := report.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertDeliveryStatus(t *testing.T, store *memoryDeliveryStore, messageID, messageState, partState string) {
	t.Helper()
	status, err := store.GetSMSDeliveryStatus(messageID)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != messageState || len(status.Parts) != 1 || status.Parts[0].State != partState {
		t.Fatalf("delivery status = %+v", status)
	}
}

func assertDeliveryEvents(t *testing.T, subscriber *captureIMSEventSubscriber, messageID string, types ...string) {
	t.Helper()
	for _, want := range types {
		select {
		case event := <-subscriber.events:
			if event.Type() != want || event.DeviceID() != "wwan0" || deliveryEventMessageID(event) != messageID {
				t.Fatalf("event = %#v, want %s for %s", event, want, messageID)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing %s event for %s", want, messageID)
		}
	}
}

func deliveryEventMessageID(event events.Event) string {
	switch value := event.(type) {
	case *events.EventSMSDeliveryUpdated:
		return value.MessageID
	case *events.EventSMSDeliveryCompleted:
		return value.MessageID
	case *events.EventSMSDeliveryFailed:
		return value.MessageID
	default:
		return ""
	}
}
