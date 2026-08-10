package imscore

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

func TestDialogHandleRetainsOriginalFieldPrefix(t *testing.T) {
	typeOf := reflect.TypeOf(imscoreDialogHandle{})
	want := []string{"id", "client", "server"}
	for index, name := range want {
		if got := typeOf.Field(index).Name; got != name {
			t.Fatalf("field %d = %q, want %q", index, got, name)
		}
	}
	if reflect.TypeOf(imsendpoint.DialogRequestOptions{}).Field(0).Type.Kind() != reflect.Int64 {
		t.Fatal("DialogRequestOptions.Timeout is not int64")
	}
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.StopCurrent)
	handle := &imscoreDialogHandle{id: "empty-dialog"}
	service.dialogs().store(handle)
	if err := service.CloseDialog(t.Context(), service.DeviceID(), handle); err != nil {
		t.Fatal(err)
	}
	if service.dialogs().len() != 0 {
		t.Fatal("CloseDialog retained an empty session handle")
	}
	if service.NextCSeq() != 1 || service.NextCSeq() != 2 {
		t.Fatal("NextCSeq did not atomically advance")
	}
}

func TestSendDialogRequestOwnsHeadersAndSerializesCSeq(t *testing.T) {
	service := newRegisteredClientInviteService(t)
	dialog := testClientDialog(t, service, "dialog-send")
	writes := make(chan string, 32)
	service.transport.SetSendFn(func(request string) error {
		writes <- request
		service.transport.DeliverResponse(mustTransactionResponse(t, request, 200))
		return nil
	})
	template := testDialogTemplate(t, sip.INFO)
	response, err := service.SendDialogRequest(
		t.Context(), service.DeviceID(), dialog, template,
		imsendpoint.DialogRequestOptions{Timeout: int64(time.Second)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response == nil || response.StatusCode != 200 {
		t.Fatalf("response = %+v", response)
	}
	request := waitTransactionWrite(t, writes)
	assertRestoredDialogRequest(t, request, dialog, "2 INFO")

	const concurrentRequests = 20
	var wait sync.WaitGroup
	errorsOut := make(chan error, concurrentRequests)
	for range concurrentRequests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, sendErr := service.SendDialogRequest(
				t.Context(), service.DeviceID(), dialog, testDialogTemplate(t, sip.UPDATE),
				imsendpoint.DialogRequestOptions{Timeout: int64(time.Second)},
			)
			errorsOut <- sendErr
		}()
	}
	wait.Wait()
	close(errorsOut)
	for sendErr := range errorsOut {
		if sendErr != nil {
			t.Fatal(sendErr)
		}
	}
	sequences := make([]int, 0, concurrentRequests)
	for range concurrentRequests {
		request = waitTransactionWrite(t, writes)
		sequence, method, parseErr := parseSIPCSeq(rawSIPHeaderValue(request, "CSeq"))
		if parseErr != nil || method != "UPDATE" {
			t.Fatalf("CSeq = %q: %v", rawSIPHeaderValue(request, "CSeq"), parseErr)
		}
		sequences = append(sequences, sequence)
	}
	sort.Ints(sequences)
	for index, sequence := range sequences {
		if want := index + 3; sequence != want {
			t.Fatalf("sequence[%d] = %d, want %d", index, sequence, want)
		}
	}
}

func TestSendDialogRequestUsesProductionRegistrationSocket(t *testing.T) {
	service, registrar, dialog := newRegisteredUDPDialogService(t)
	serverResult := make(chan error, 1)
	go serveOneDialogTransaction(registrar, serverResult)

	response, err := service.SendDialogRequest(
		t.Context(), service.DeviceID(), dialog, testDialogTemplate(t, sip.INFO),
		imsendpoint.DialogRequestOptions{Timeout: int64(time.Second)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response == nil || response.StatusCode != 200 {
		t.Fatalf("response = %+v", response)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestInboundDialogBYERespondsOnceAndCloses(t *testing.T) {
	service := newServerTransactionTestService(t)
	dialog := testClientDialog(t, service, "inbound-bye")
	var captured InboundVoiceRequest
	service.SetVoiceRequestHandler(&serverTransactionVoiceHandler{handle: func(
		request InboundVoiceRequest,
	) (InboundVoiceResult, error) {
		captured = request
		return InboundVoiceResult{Handled: true, StatusCode: 200}, nil
	}})
	recorder := &serverResponseRecorder{}
	request := testInboundDialogRequest("BYE", dialog, 2)
	if err := service.dispatchInboundSIP(request, recorder.reply); err != nil {
		t.Fatal(err)
	}
	waitForServerCondition(t, func() bool { return len(recorder.snapshot()) == 1 })
	if !captured.DialogMatched || !captured.DialogResponded || !captured.DialogTerminated {
		t.Fatalf("dialog routing flags = %+v", captured)
	}
	if captured.Dialog == nil || captured.Dialog.DialogID() != dialog.id {
		t.Fatalf("routed dialog = %v", captured.Dialog)
	}
	if response := recorder.snapshot()[0]; !strings.HasPrefix(response, "SIP/2.0 200 OK") {
		t.Fatalf("BYE response = %q", response)
	}
	if service.dialogs().load(dialog.id) != nil || !dialog.closed {
		t.Fatal("terminated dialog remained live")
	}
}

func TestServerDialogBYEWaitsForACK(t *testing.T) {
	service := newRegisteredClientInviteService(t)
	dialog := testServerDialog(t, service, "server-dialog-bye")
	writes := make(chan string, 2)
	service.transport.SetSendFn(func(request string) error {
		writes <- request
		service.transport.DeliverResponse(mustTransactionResponse(t, request, 200))
		return nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, err := service.SendDialogRequest(
		ctx, service.DeviceID(), dialog, testDialogTemplate(t, sip.BYE),
		imsendpoint.DialogRequestOptions{},
	)
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("unconfirmed BYE error = %v", err)
	}
	assertNoTransactionWrite(t, writes, 10*time.Millisecond)
	ack := mustServerTestRequest(t, testInboundDialogRequest("ACK", dialog, 1))
	if err := readInboundDialogACK(dialog, ack); err != nil {
		t.Fatal(err)
	}
	response, err := service.SendDialogRequest(
		t.Context(), service.DeviceID(), dialog, testDialogTemplate(t, sip.BYE),
		imsendpoint.DialogRequestOptions{Timeout: int64(time.Second)},
	)
	if err != nil || response != nil {
		t.Fatalf("server BYE response=%v error=%v", response, err)
	}
	written := waitTransactionWrite(t, writes)
	if cseq := rawSIPHeaderValue(written, "CSeq"); cseq != "2 BYE" {
		t.Fatalf("server BYE CSeq = %q", cseq)
	}
	if service.dialogs().load(dialog.id) == nil {
		t.Fatal("outbound BYE removed dialog before CloseDialog")
	}
	if err := service.CloseDialog(t.Context(), service.DeviceID(), dialog); err != nil {
		t.Fatal(err)
	}
	if service.dialogs().load(dialog.id) != nil {
		t.Fatal("CloseDialog did not remove dialog")
	}
}

func TestReliableProvisionalPRACKUsesEarlyDialog(t *testing.T) {
	service := newRegisteredClientInviteService(t)
	writes := recordTransactionWrites(service.transport)
	started := make(chan imsendpoint.InviteHandle, 1)
	provisional := make(chan struct{}, 1)
	result := make(chan legacyClientInviteOutcome, 1)
	request := mustClientInviteRequest(t, "early-prack")
	go func() {
		value, err := service.StartClientInvite(
			context.Background(), service.DeviceID(), imsendpoint.ClientInviteOptions{
				Request: request, Contact: request.Contact(),
				OnStarted: func(handle imsendpoint.InviteHandle) error {
					started <- handle
					return nil
				},
				OnResponse: func(response *sip.Response) error {
					if response.StatusCode == 183 {
						provisional <- struct{}{}
					}
					return nil
				},
			},
		)
		result <- legacyClientInviteOutcome{result: value, err: err}
	}()
	writtenInvite := waitTransactionWrite(t, writes)
	invite := <-started
	earlyResponse := testReliableInviteResponse(t, writtenInvite, 183)
	service.transport.DeliverResponse(earlyResponse)
	<-provisional

	prackResult := make(chan error, 1)
	go func() {
		prackResult <- service.SendReliableProvisionalPRACK(
			context.Background(), service.DeviceID(), imsendpoint.ReliableProvisionalOptions{
				Invite: invite, RSeq: "41", Contact: "<sip:callee@192.0.2.20:5060>",
			},
		)
	}()
	writtenPRACK := waitTransactionWrite(t, writes)
	if sipRequestMethod(writtenPRACK) != "PRACK" {
		t.Fatalf("method = %q", sipRequestMethod(writtenPRACK))
	}
	if rack := rawSIPHeaderValue(writtenPRACK, "RAck"); rack != "41 1 INVITE" {
		t.Fatalf("RAck = %q", rack)
	}
	if cseq := rawSIPHeaderValue(writtenPRACK, "CSeq"); cseq != "2 PRACK" {
		t.Fatalf("PRACK CSeq = %q", cseq)
	}
	service.transport.DeliverResponse(mustTransactionResponse(t, writtenPRACK, 200))
	if err := <-prackResult; err != nil {
		t.Fatal(err)
	}
	service.transport.DeliverResponse(testReliableInviteResponse(t, writtenInvite, 200))
	outcome := <-result
	if outcome.err != nil || outcome.result == nil || outcome.result.Dialog == nil {
		t.Fatalf("INVITE outcome = %+v, error = %v", outcome.result, outcome.err)
	}
}

func TestRetainClientInviteEarlyDialogReplacesForkedTag(t *testing.T) {
	service := newRegisteredClientInviteService(t)
	request := mustClientInviteRequest(t, "forked-early")
	invite := &imscoreInviteHandle{
		initialRequest: request,
		transaction:    &clientSIPTransaction{send: func(string) error { return nil }},
	}
	firstResponse := testReliableInviteResponse(t, request.String(), 183)
	setResponseToTag(firstResponse.parsed, "early-a")
	service.retainClientInviteEarlyDialog(invite, firstResponse)
	firstID := clientInviteDialogID(invite)
	first := service.dialogs().load(firstID)
	if first == nil {
		t.Fatal("first early dialog was not retained")
	}

	secondResponse := testReliableInviteResponse(t, request.String(), 183)
	setResponseToTag(secondResponse.parsed, "early-b")
	service.retainClientInviteEarlyDialog(invite, secondResponse)
	secondID := clientInviteDialogID(invite)
	if secondID == firstID || service.dialogs().load(secondID) == nil {
		t.Fatalf("replacement early dialog ID = %q, first = %q", secondID, firstID)
	}
	if service.dialogs().load(firstID) != nil || !first.closed {
		t.Fatal("replaced early dialog remained live")
	}
	if service.dialogs().len() != 1 {
		t.Fatalf("dialog registry size = %d, want 1", service.dialogs().len())
	}
}
