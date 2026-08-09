package imscore

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/smscodec"
)

var (
	_ func(*Service, context.Context, string, string) (SendOutcome, error)              = (*Service).SendSMSWithResult
	_ func(*Service, context.Context, string, string, SendOptions) (SendOutcome, error) = (*Service).SendSMSWithOptions
	_ func(*Service, string) (*DeliveryStatus, error)                                   = (*Service).GetSMSDeliveryStatus
)

func TestOutboundDispatchShardIndexMatchesFNV1a(t *testing.T) {
	for _, key := range []string{"", "callid:sms-1", "fallback:mo-submit:MESSAGE"} {
		hash := fnv.New32a()
		_, _ = hash.Write([]byte(key))
		want := int(hash.Sum32() % outboundRequestShardCount)
		if got := outboundDispatchShardIndex(key, outboundRequestShardCount); got != want {
			t.Fatalf("index(%q) = %d, want %d", key, got, want)
		}
	}
}

func TestOutboundDispatcherPreservesCallIDOrder(t *testing.T) {
	service, _, _ := newOutboundSMSTestService(t)
	sent := make(chan string, 3)
	service.transport.SetSendFn(func(raw string) error {
		sent <- raw
		return nil
	})
	for sequence := 1; sequence <= 3; sequence++ {
		request := parsedDispatchRequest(t, "ordered-call", sequence)
		if _, _, err := service.dispatchOutboundRequest(context.Background(), "test", request, time.Second, false); err != nil {
			t.Fatal(err)
		}
	}
	for sequence := 1; sequence <= 3; sequence++ {
		raw := waitForOutboundSMSControl(t, sent)
		if got := rawSIPHeaderValue(raw, "CSeq"); got != fmt.Sprintf("%d MESSAGE", sequence) {
			t.Fatalf("CSeq = %q, want sequence %d", got, sequence)
		}
		service.transport.DeliverResponse(registerResponseForRequest(raw, 200, nil))
	}
}

func TestOutboundDispatcherRejectsFullShard(t *testing.T) {
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Stop)
	request := parsedDispatchRequest(t, "full-call", 1)
	shards := make([]chan outboundRequestTask, outboundRequestShardCount)
	for index := range shards {
		shards[index] = make(chan outboundRequestTask, 1)
	}
	index := outboundDispatchShardIndex(outboundDispatchKey(request, "test"), len(shards))
	shards[index] <- outboundRequestTask{}
	service.outboundReqShards = shards
	_, _, err = service.dispatchOutboundRequest(context.Background(), "test", request, time.Second, false)
	if !errors.Is(err, errOutboundRequestQueueFull) || service.outboundQueueReject.Load() != 1 {
		t.Fatalf("queue error = %v, rejects = %d", err, service.outboundQueueReject.Load())
	}
}

func TestPendingSMSMatchesNormalizedCallIDAndRPMR(t *testing.T) {
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Stop)
	first := &smsPendingInfo{RPMR: 7, RespCh: make(chan smsSendResult, 1), CreatedAt: time.Now()}
	service.registerPendingSMS(" <ABC@IMS.EXAMPLE> ", first)
	if got := service.takePendingSMSByCallID("abc@ims.example"); got != first {
		t.Fatalf("normalized Call-ID matched %p, want %p", got, first)
	}
	second := &smsPendingInfo{RPMR: 9, RespCh: make(chan smsSendResult, 1), CreatedAt: time.Now()}
	service.registerPendingSMS("other-call", second)
	matched, ok := service.completePendingSMSByReport("", "", 9, smsSendResult{Status: "acked"})
	if !ok || matched != second || (<-second.RespCh).Status != "acked" {
		t.Fatalf("RP-MR match = %p, ok=%v", matched, ok)
	}
}

func TestMTSMSFingerprintReservationIsConcurrentAndBounded(t *testing.T) {
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Stop)
	const callers = 64
	var accepted int
	var mu sync.Mutex
	var workers sync.WaitGroup
	for range callers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if service.reserveMTSMSFingerprint("same-message", time.Now()) {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}()
	}
	workers.Wait()
	if accepted != 1 || service.mtSMSDedupHit.Load() != callers-1 {
		t.Fatalf("accepted=%d dedup=%d", accepted, service.mtSMSDedupHit.Load())
	}
}

func TestInboundSMSDeduplicatesAcrossSIPTransactions(t *testing.T) {
	service, subscriber, outbound := newInboundSMSTestService(t)
	body := inboundRPData(t, 0x25, "+447700900123", "one delivery")
	first := strings.Replace(inboundSMSRequest(t, imsSMSContentType, body), "Call-ID: inbound-sms", "Call-ID: duplicate-a", 1)
	second := strings.Replace(inboundSMSRequest(t, imsSMSContentType, body), "Call-ID: inbound-sms", "Call-ID: duplicate-b", 1)
	dispatchInboundRaw(t, service, first)
	dispatchInboundRaw(t, service, second)
	select {
	case <-subscriber.events:
	case <-time.After(time.Second):
		t.Fatal("first SMS was not dispatched")
	}
	select {
	case duplicate := <-subscriber.events:
		t.Fatalf("duplicate event = %#v", duplicate)
	case <-time.After(30 * time.Millisecond):
	}
	_ = waitForOutboundSMSControl(t, outbound)
	_ = waitForOutboundSMSControl(t, outbound)
	if service.mtSMSDedupHit.Load() != 1 {
		t.Fatalf("dedup hits = %d", service.mtSMSDedupHit.Load())
	}
}

func TestFragmentStateCompletesOutOfOrderAndAuditsCollision(t *testing.T) {
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Stop)
	second := &smsFragment{Ref: 4, Total: 2, Seq: 2, Content: "world", Time: time.Now()}
	if _, complete, err := service.handleSMSFragment("+1234567", second); err != nil || complete {
		t.Fatalf("second fragment complete=%v err=%v", complete, err)
	}
	first := &smsFragment{Ref: 4, Total: 2, Seq: 1, Content: "hello ", Time: time.Now()}
	text, complete, err := service.handleSMSFragment("+1234567", first)
	if err != nil || !complete || text != "hello world" {
		t.Fatalf("assembled=%q complete=%v err=%v", text, complete, err)
	}
	base := &smsFragment{Ref: 5, Total: 2, Seq: 1, Content: "old", Time: time.Now()}
	_, _, _ = service.handleSMSFragment("+1234567", base)
	_, _, err = service.handleSMSFragment("+1234567", &smsFragment{Ref: 5, Total: 2, Seq: 1, Content: "new", Time: time.Now()})
	if err == nil || len(service.fragmentAuditSnapshot()["audit_failures"].([]fragmentAuditFailure)) != 1 {
		t.Fatalf("collision error=%v audit=%v", err, service.fragmentAuditSnapshot())
	}
}

func TestRPAckRetriesAndRecordsSuccess(t *testing.T) {
	service, _, _ := newInboundSMSTestService(t)
	attempts := 0
	outbound := make(chan string, 1)
	service.transport.SetSendFn(func(raw string) error {
		attempts++
		if attempts < 3 {
			return syscall.EAGAIN
		}
		outbound <- raw
		service.transport.DeliverResponse(registerResponseForRequest(raw, 200, nil))
		return nil
	})
	raw := inboundSMSRequest(t, imsSMSContentType, inboundRPData(t, 0x31, "+447700900123", "ack"))
	service.sendRpAckWithRetryPolicy(raw, smscodec.BuildRPAck(0x31), 0x31, "fingerprint", time.Millisecond, time.Millisecond)
	if attempts != 3 || service.mtAckSendOK.Load() != 1 || service.mtAckSendErr.Load() != 2 {
		t.Fatalf("attempts=%d ok=%d err=%d", attempts, service.mtAckSendOK.Load(), service.mtAckSendErr.Load())
	}
}

func TestOutboundSMSUsesProductionUDPSocket(t *testing.T) {
	registrar, client, service := newRealUDPSMSService(t)
	serverErr := make(chan error, 1)
	requestCh := make(chan string, 1)
	go serveSingleSMSMessage(registrar, requestCh, serverErr)
	outcome, err := service.SendSMSWithResult(context.Background(), "+447700900123", "socket")
	if err != nil || outcome.MessageID == "" || outcome.DeliveryState != smsDeliveryStateAcked {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	select {
	case raw := <-requestCh:
		if sipRequestMethod(raw) != "MESSAGE" || rawSIPHeaderValue(raw, "Content-Type") != imsSMSContentType {
			t.Fatalf("request = %q", raw)
		}
	case <-time.After(time.Second):
		t.Fatal("real UDP MESSAGE was not received")
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
	_ = client
}

func TestSMSReadyCallbackFiresOnce(t *testing.T) {
	service, err := New(&IMSConfig{DeviceID: "ready", SMSC: "+447802002606"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Stop)
	called := 0
	service.SetOnSMSReady(func() { called++ })
	service.setSMSReceiverReady(true)
	if called != 0 {
		t.Fatalf("callback before registration = %d", called)
	}
	service.regStatus.Store(registrationRegistered)
	service.notifySMSReadiness()
	service.notifySMSReadiness()
	service.setSMSReceiverReady(true)
	if called != 1 {
		t.Fatalf("callback count = %d", called)
	}
}

func TestRecoveredSMSNotificationFormats(t *testing.T) {
	at := time.Date(2026, time.August, 9, 12, 34, 56, 0, time.UTC)
	sent := formatVoWiFiSMSSentMessage("wwan0", "+447700900123", "hello", at, 2)
	wantSent := "发送短信 / 完成\n设备    wwan0\n号码    +447700900123\n通道    VoWiFi\n时间    2026-08-09 12:34:56\n内容    hello\n分片    2"
	if sent != wantSent {
		t.Fatalf("sent notification = %q", sent)
	}
	incomplete := formatVoWiFiIncompleteSMSMessage(
		"wwan0", "+447700900123", "part", at, 1, 3, "2,3",
	)
	wantIncomplete := "收到新短信 / VoWiFi\n设备  wwan0\n号码  +447700900123\n时间  2026-08-09 12:34:56\n内容  part\n状态  分片不完整 1/3，已降级拼接\n缺失  2,3"
	if incomplete != wantIncomplete {
		t.Fatalf("incomplete notification = %q", incomplete)
	}
}

func TestFragmentTimeoutDegradesAndAudits(t *testing.T) {
	service, subscriber, _, _ := newDeliveryReportTestService(t)
	fragment := &smsFragment{
		Ref: 7, Total: 2, Seq: 1, Content: "first", RpMr: 4,
		CallID: "fragment-call", ToURI: "sip:user@ims.example",
		Time: time.Now().Add(-time.Second),
	}
	if _, complete, err := service.handleSMSFragment("+447700900123", fragment); err != nil || complete {
		t.Fatalf("fragment complete=%v err=%v", complete, err)
	}
	service.cleanupExpiredFragments(time.Millisecond)
	assertIMSEventTypes(t, subscriber, "LogNotify", "SMSReceived")
	snapshot := service.fragmentAuditSnapshot()
	failures, ok := snapshot["audit_failures"].([]fragmentAuditFailure)
	if snapshot["timeout_degrade"] != int64(1) || !ok || len(failures) != 1 || failures[0].MissingSeq != "2" {
		t.Fatalf("fragment audit = %#v", snapshot)
	}
}

func parsedDispatchRequest(t *testing.T, callID string, sequence int) *sip.Request {
	t.Helper()
	raw := strings.Replace(transactionRequest("MESSAGE", callID), "CSeq: 1 MESSAGE", fmt.Sprintf("CSeq: %d MESSAGE", sequence), 1)
	message, err := parseSIPMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	return message.(*sip.Request)
}

func dispatchInboundRaw(t *testing.T, service *Service, raw string) {
	t.Helper()
	if err := service.dispatchInboundSIP(raw, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func newRealUDPSMSService(t *testing.T) (*net.UDPConn, *net.UDPConn, *Service) {
	t.Helper()
	registrar, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(&IMSConfig{
		DeviceID: "udp-sms", IMSI: "234100000000001", IMPI: "234100000000001@ims.example",
		IMPU: "sip:234100000000001@ims.example", Domain: "ims.example", SMSC: "+447802002606",
		LocalIP: net.IPv4(127, 0, 0, 1), LocalPort: client.LocalAddr().(*net.UDPAddr).Port, Transport: "udp",
	})
	if err != nil {
		t.Fatal(err)
	}
	remote := registrar.LocalAddr().(*net.UDPAddr)
	service.mu.Lock()
	service.regState, service.smsReceiverReady = regRegistered, true
	service.registrationIO, service.registrationRemote = client, cloneUDPAddr(remote)
	service.registrationTransport = "udp"
	service.regSession = &registerSession{publicID: "sip:234100000000001@ims.example", contactUser: "udp-sms", cseq: 1}
	service.mu.Unlock()
	service.regStatus.Store(registrationRegistered)
	service.activateInitialSendAndReceive(&initialRegistrationTransport{kind: "udp", remote: remote, packet: client, port: client.LocalAddr().(*net.UDPAddr).Port})
	t.Cleanup(service.Stop)
	t.Cleanup(func() { _ = registrar.Close() })
	return registrar, client, service
}

func serveSingleSMSMessage(conn *net.UDPConn, requestCh chan<- string, result chan<- error) {
	buffer := make([]byte, 64*1024)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	n, remote, err := conn.ReadFromUDP(buffer)
	if err != nil {
		result <- err
		return
	}
	request := string(buffer[:n])
	requestCh <- request
	_, err = conn.WriteToUDP([]byte(transactionResponseWire(request, 200, "OK")), remote)
	result <- err
}
