package imscore

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/warthog618/sms/encoding/tpdu"
)

type captureIMSEventSubscriber struct {
	events chan events.Event
}

func (s *captureIMSEventSubscriber) OnIMSEvent(event events.Event) {
	s.events <- event
}

func TestInboundSMSDeliversEventAndSendsRPAck(t *testing.T) {
	service, subscriber, outbound := newInboundSMSTestService(t)
	rpMR := byte(0x33)
	raw := inboundSMSRequest(t, imsSMSContentType, inboundRPData(t, rpMR, "+447700900123", "hello"))
	var response string
	if err := service.dispatchInboundSIP(raw, func(value string) error {
		response = value
		return nil
	}); err != nil {
		t.Fatalf("dispatchInboundSIP: %v", err)
	}
	if !strings.HasPrefix(response, "SIP/2.0 200") {
		t.Fatalf("SIP response = %q", response)
	}

	select {
	case event := <-subscriber.events:
		received, ok := event.(*events.EventSMSReceived)
		if !ok || !strings.HasSuffix(received.Sender, "447700900123") || received.Content != "hello" {
			t.Fatalf("received event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("inbound SMS event was not published")
	}

	request := waitForOutboundSMSControl(t, outbound)
	body, err := rawSIPBody(request)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(smscodec.BuildRPAck(rpMR)) {
		t.Fatalf("RP-ACK body = %x", body)
	}
}

func TestInboundMalformedSMSReturnsProtocolErrorWithoutEvent(t *testing.T) {
	service, subscriber, outbound := newInboundSMSTestService(t)
	raw := inboundSMSRequest(t, imsSMSContentType, []byte{0x01, 0x44, 0x08, 0x91})
	var response string
	err := service.dispatchInboundSIP(raw, func(value string) error {
		response = value
		return nil
	})
	if err == nil || !strings.HasPrefix(response, "SIP/2.0 400") {
		t.Fatalf("dispatch error = %v, response = %q", err, response)
	}
	select {
	case event := <-subscriber.events:
		t.Fatalf("malformed SMS published event %#v", event)
	default:
	}
	request := waitForOutboundSMSControl(t, outbound)
	body, bodyErr := rawSIPBody(request)
	if bodyErr != nil {
		t.Fatal(bodyErr)
	}
	if info := smscodec.ClassifyRPDU(body); info.Kind != smscodec.RPDUKindError || info.MR != 0x44 {
		t.Fatalf("RP-ERROR body = %x", body)
	}
}

func TestInboundSMSRejectsUnsupportedContentType(t *testing.T) {
	service, subscriber, outbound := newInboundSMSTestService(t)
	raw := inboundSMSRequest(t, "text/plain", []byte("hello"))
	var response string
	if err := service.dispatchInboundSIP(raw, func(value string) error {
		response = value
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(response, "SIP/2.0 415") {
		t.Fatalf("SIP response = %q", response)
	}
	select {
	case event := <-subscriber.events:
		t.Fatalf("unsupported MESSAGE published event %#v", event)
	case request := <-outbound:
		t.Fatalf("unsupported MESSAGE sent RP control %q", request)
	default:
	}
}

func newInboundSMSTestService(t *testing.T) (*Service, *captureIMSEventSubscriber, <-chan string) {
	t.Helper()
	bus := NewEventBus()
	subscriber := &captureIMSEventSubscriber{events: make(chan events.Event, 2)}
	bus.Subscribe(subscriber)
	service, err := New(&IMSConfig{
		DeviceID: "wwan0", IMSI: "234102356143376", IMPI: "234102356143376@ims.example",
		IMPU: []string{"sip:234102356143376@ims.example"}, Domain: "ims.example", SMSC: "+447802002606",
		LocalIP: net.IPv4(10, 0, 0, 2), LocalPort: 5060, Transport: "tcp", EventBus: bus,
	})
	if err != nil {
		t.Fatal(err)
	}
	outbound := make(chan string, 2)
	service.transport.SetSendFn(func(request string) error {
		outbound <- request
		service.transport.DeliverResponse(registerResponseForRequest(request, 200, nil))
		return nil
	})
	t.Cleanup(service.Stop)
	return service, subscriber, outbound
}

func waitForOutboundSMSControl(t *testing.T, outbound <-chan string) string {
	t.Helper()
	select {
	case request := <-outbound:
		return request
	case <-time.After(time.Second):
		t.Fatal("RP control MESSAGE was not sent")
		return ""
	}
}

func inboundSMSRequest(t *testing.T, contentType string, body []byte) string {
	t.Helper()
	return fmt.Sprintf("MESSAGE sip:234102356143376@ims.example SIP/2.0\r\n"+
		"Via: SIP/2.0/TCP 10.0.0.1:5060;branch=z9hG4bK-inbound\r\n"+
		"From: <sip:+447802002606@ims.example>;tag=remote\r\n"+
		"To: <sip:234102356143376@ims.example>\r\n"+
		"Call-ID: inbound-sms\r\nCSeq: 1 MESSAGE\r\n"+
		"Content-Type: %s\r\nContent-Length: %d\r\n\r\n%s", contentType, len(body), body)
}

func inboundRPData(t *testing.T, mr byte, sender, text string) []byte {
	t.Helper()
	originator, err := smscodec.EncodeAddress("+447802002606")
	if err != nil {
		t.Fatal(err)
	}
	tpduBytes := deliverTPDU(t, sender, text)
	body := []byte{0x01, mr}
	body = append(body, originator...)
	body = append(body, 0x00, byte(len(tpduBytes)))
	return append(body, tpduBytes...)
}

func deliverTPDU(t *testing.T, sender, text string) []byte {
	t.Helper()
	pdu, err := tpdu.NewDeliver(tpdu.WithOA(tpdu.NewAddress(tpdu.FromNumber(sender))))
	if err != nil {
		t.Fatal(err)
	}
	pdu.SetPID(0)
	pdu.SetDCS(0)
	userData, _, _ := tpdu.EncodeUserData([]byte(text))
	pdu.SetUD(userData)
	raw, err := pdu.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
