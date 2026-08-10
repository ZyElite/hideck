package imscore

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/warthog618/sms/encoding/tpdu"
)

func TestOutboundMultipartSMSUsesCorrelatedReferences(t *testing.T) {
	service, subscriber, store, outbound := newDeliveryReportTestService(t)
	service.smsRandom = bytes.NewReader([]byte{0xa0, 0xa1, 0xa2, 0xa3, 0xb0, 0xb1, 0xb2, 0xb3})
	text := strings.Repeat("multipart ", 40)
	startedAt := time.Now()
	outcome, err := service.SendSMSWithResult(context.Background(), "+447700900123", text)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.PartsTotal < 2 {
		t.Fatalf("parts total = %d", outcome.PartsTotal)
	}
	wantDelay := time.Duration(outcome.PartsTotal-1) * outboundSMSInterPartDelay
	if elapsed := time.Since(startedAt); elapsed < wantDelay {
		t.Fatalf("multipart elapsed = %s, want at least %s", elapsed, wantDelay)
	}

	concatReference := -1
	for partNo := 1; partNo <= outcome.PartsTotal; partNo++ {
		request := waitForOutboundSMSControl(t, outbound)
		body, bodyErr := rawSIPBody(request)
		if bodyErr != nil {
			t.Fatal(bodyErr)
		}
		rpMR, _, _, rawTPDU, parseErr := smscodec.ParseRPDataWithAddresses(body)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		decoded := &tpdu.TPDU{Direction: tpdu.MO}
		if unmarshalErr := decoded.UnmarshalBinary(rawTPDU); unmarshalErr != nil {
			t.Fatal(unmarshalErr)
		}
		total, sequence, reference, ok := decoded.ConcatInfo()
		if !ok || total != outcome.PartsTotal || sequence != partNo {
			t.Fatalf("part %d concat=(%d,%d,%d,%v)", partNo, total, sequence, reference, ok)
		}
		if concatReference == -1 {
			concatReference = reference
		} else if reference != concatReference {
			t.Fatalf("part %d reference = %d, want %d", partNo, reference, concatReference)
		}
		stored := store.part(outcome.MessageID, partNo)
		wantRPMR := 0xa0 + partNo - 1
		if stored.rpMR != wantRPMR || int(rpMR) != wantRPMR {
			t.Fatalf("part %d RP-MR body=%d stored=%d want=%d", partNo, rpMR, stored.rpMR, wantRPMR)
		}
		if int(decoded.MR) == stored.rpMR {
			t.Fatalf("part %d reused RP-MR %d as TP-MR", partNo, stored.rpMR)
		}
		if int(decoded.MR) != partNo {
			t.Fatalf("part %d TP-MR=%d want=%d", partNo, decoded.MR, partNo)
		}
	}
	if concatReference != 1 {
		t.Fatalf("concat reference=%d want=1", concatReference)
	}
	accepted := <-subscriber.events
	if accepted.Type() != "SMSSendAccepted" {
		t.Fatalf("first event = %#v", accepted)
	}
	for range outcome.PartsTotal {
		if event := <-subscriber.events; event.Type() != "SMSDeliveryUpdated" {
			t.Fatalf("delivery event = %#v", event)
		}
	}
	assertIMSEventTypes(t, subscriber, "LogNotify")
	event := <-subscriber.events
	sent, ok := event.(*events.EventSMSSent)
	if !ok || sent.Content != text || sent.TotalParts != outcome.PartsTotal {
		t.Fatalf("sent event = %#v", event)
	}
	second, err := service.SendSMSWithResult(context.Background(), "+447700900123", text)
	if err != nil {
		t.Fatal(err)
	}
	secondReference := -1
	for range second.PartsTotal {
		request := waitForOutboundSMSControl(t, outbound)
		body, _ := rawSIPBody(request)
		_, _, _, rawTPDU, _ := smscodec.ParseRPDataWithAddresses(body)
		decoded := &tpdu.TPDU{Direction: tpdu.MO}
		if err := decoded.UnmarshalBinary(rawTPDU); err != nil {
			t.Fatal(err)
		}
		_, _, secondReference, _ = decoded.ConcatInfo()
	}
	if secondReference != concatReference {
		t.Fatalf("default concat reference changed: first=%d second=%d", concatReference, secondReference)
	}
}

func TestInboundMultipartSMSPublishesOnceAfterOutOfOrderCompletion(t *testing.T) {
	service, subscriber, _, outbound := newDeliveryReportTestService(t)
	text := strings.Repeat("inbound multipart ", 14)
	parts := multipartDeliverTPDUs(t, "+447700900123", text)
	if len(parts) != 2 {
		t.Fatalf("deliver parts = %d, want 2", len(parts))
	}

	dispatchInboundMultipartPart(t, service, parts[1], 0x42)
	select {
	case event := <-subscriber.events:
		t.Fatalf("partial multipart SMS published: %#v", event)
	default:
	}
	dispatchInboundMultipartPart(t, service, parts[0], 0x41)
	select {
	case event := <-subscriber.events:
		received, ok := event.(*events.EventSMSReceived)
		if !ok || received.Content != text || received.Sender != "+447700900123" {
			t.Fatalf("received event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("completed multipart SMS was not published")
	}

	dispatchInboundMultipartPart(t, service, parts[0], 0x43)
	select {
	case event := <-subscriber.events:
		t.Fatalf("duplicate multipart SMS published: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
	for range 3 {
		_ = waitForOutboundSMSControl(t, outbound)
	}
}

func multipartDeliverTPDUs(t *testing.T, sender, text string) [][]byte {
	t.Helper()
	submits, _, err := smscodec.BuildSubmitTPDUsWithOptions("+447700900000", text, smscodec.SubmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result := make([][]byte, 0, len(submits))
	for index := range submits {
		submit := tpdu.TPDU{Direction: tpdu.MO}
		if unmarshalErr := submit.UnmarshalBinary(submits[index]); unmarshalErr != nil {
			t.Fatal(unmarshalErr)
		}
		deliver, newErr := tpdu.NewDeliver(tpdu.WithOA(tpdu.NewAddress(tpdu.FromNumber(sender))))
		if newErr != nil {
			t.Fatal(newErr)
		}
		deliver.SetPID(submit.PID)
		deliver.SetDCS(byte(submit.DCS))
		deliver.SetUDH(submit.UDH)
		deliver.SetUD(submit.UD)
		raw, marshalErr := deliver.MarshalBinary()
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		result = append(result, raw)
	}
	return result
}

func dispatchInboundMultipartPart(t *testing.T, service *Service, rawTPDU []byte, rpMR byte) {
	t.Helper()
	request := inboundSMSRequest(t, imsSMSContentType, networkRPData(t, rpMR, rawTPDU))
	var response string
	if err := service.dispatchInboundSIP(request, func(value string) error {
		response = value
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(response, "SIP/2.0 200") {
		t.Fatalf("SIP response = %q", response)
	}
}
