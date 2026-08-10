package runtimehost

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/smsdelivery"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

// stubAKA is a deterministic AKA provider.
type stubAKA struct{}

func (stubAKA) CalculateAKA(rand16, autn16 []byte) (imscore.AKAResult, error) {
	return imscore.AKAResult{
		RES: []byte{0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33},
		CK:  []byte{0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11},
		IK:  []byte{0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22},
	}, nil
}

// newTestService builds an imscore service for adapter tests.
func newTestService(t *testing.T) *imscore.Service {
	t.Helper()
	return newTestServiceWithRegistrar(t, startTestRegistrar(t))
}

func newTestServiceWithRegistrar(t *testing.T, registrar *net.UDPConn) *imscore.Service {
	t.Helper()
	plainSIP := false
	cfg := &imscore.IMSConfig{
		DeviceID:        "dev-1",
		IMSI:            "310260123456789",
		IMPI:            "310260123456789@ims.example.com",
		Domain:          "ims.example.com",
		SMSC:            "+123",
		LocalIP:         net.IPv4(127, 0, 0, 1),
		Registrar:       registrar.LocalAddr().String(),
		AKAProvider:     stubAKA{},
		EnableIPSec3GPP: &plainSIP,
	}
	svc, err := imscore.New(cfg)
	if err != nil {
		t.Fatalf("imscore.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Register(ctx); err != nil {
		t.Fatalf("imscore.Register: %v", err)
	}
	return svc
}

func startTestRegistrar(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buffer := make([]byte, 64*1024)
		for {
			n, remote, readErr := conn.ReadFromUDP(buffer)
			if readErr != nil {
				return
			}
			request := string(buffer[:n])
			body := ""
			extraHeaders := ""
			if strings.HasPrefix(request, "REGISTER ") {
				extraHeaders = "P-Associated-URI: <sip:+15551234567@ims.example.com>\r\n"
			}
			if strings.HasPrefix(request, "INVITE ") {
				body = `<?xml version="1.0"?><ussd-data><language>en</language><ussd-string>Balance: 10</ussd-string><UnstructuredSS-Notify/></ussd-data>`
				extraHeaders = "To: <sip:ussi@ims.example.com>;tag=test-remote\r\n" +
					"Contact: <sip:ussi@ims.example.com>\r\n" +
					"Content-Type: application/vnd.3gpp.ussd+xml\r\n"
			}
			response := fmt.Sprintf("SIP/2.0 200 OK\r\nVia: %s\r\nCall-ID: %s\r\nCSeq: %s\r\n%sContent-Length: %d\r\n\r\n%s",
				testSIPHeader(request, "Via"), testSIPHeader(request, "Call-ID"), testSIPHeader(request, "CSeq"), extraHeaders, len(body), body)
			_, _ = conn.WriteToUDP([]byte(response), remote)
		}
	}()
	return conn
}

func testSIPHeader(message, name string) string {
	for _, line := range strings.Split(message, "\r\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func TestServiceAdapterStatus(t *testing.T) {
	svc := newTestService(t)
	adapter := newServiceAdapter(svc)
	st := adapter.Status()
	if st.State.RegStatus != 1 {
		t.Errorf("reg status = %d, want 1 (registered)", st.State.RegStatus)
	}
	if !st.State.IMSReady {
		t.Error("IMSReady should be true")
	}
	if !st.State.SMSReady {
		t.Errorf("SMSReady should be true: %s", st.State.SMSReadyReason)
	}
	if st.State.DeviceID != "dev-1" {
		t.Errorf("device = %q", st.State.DeviceID)
	}
}

func TestServiceAdapterSMS(t *testing.T) {
	svc := newTestService(t)
	adapter := newServiceAdapter(svc)
	out, err := adapter.SendSMSWithResult(context.Background(), "+8613800000000", "hi")
	if err != nil {
		t.Fatalf("SendSMSWithResult: %v", err)
	}
	if out.Ref == "" {
		t.Error("SMS ref should not be empty")
	}
}

func TestServiceAdapterUSSD(t *testing.T) {
	svc := newTestService(t)
	adapter := newServiceAdapter(svc)
	res, err := adapter.SendUSSD(context.Background(), "*100#")
	if err != nil {
		t.Fatalf("SendUSSD: %v", err)
	}
	if res.Status != 0 || res.Text != "Balance: 10" {
		t.Errorf("ussd result = %+v", res)
	}
}

func TestServiceAdapterNoService(t *testing.T) {
	adapter := newServiceAdapter(nil)
	if _, err := adapter.SendSMSWithResult(context.Background(), "1", "x"); !errors.Is(err, errNoService) {
		t.Errorf("err = %v, want errNoService", err)
	}
}

func TestAdaptSMSSendOutcomePreservesIdentity(t *testing.T) {
	out := adaptSMSSendOutcome(imscore.SendOutcome{
		MessageID: "sms-1", PartsTotal: 1, DeliveryState: "failed",
	})
	if out.MessageID != "sms-1" || out.Ref != "sms-1" || out.PartsTotal != 1 {
		t.Fatalf("delivery identity = %+v", out)
	}
	if out.DeliveryState != "failed" || out.Err != nil {
		t.Fatalf("delivery failure = %+v", out)
	}
}

func TestVoiceAgentAttachAndStopCleanup(t *testing.T) {
	svc := newTestService(t)
	gateway := voicehost.NewGateway()
	if err := gateway.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	inst := &Instance{}
	inst.setService(newServiceAdapter(svc))
	req := StartRequest{DeviceID: "dev-1", VoiceGateway: gateway}
	if err := attachVoiceAgent(req, inst, newServiceAdapter(svc)); err != nil {
		t.Fatalf("attachVoiceAgent: %v", err)
	}
	if gateway.GetAgent("dev-1") == nil || gateway.DeviceStatus("dev-1")["ready"] != true {
		t.Fatalf("voice status = %+v", gateway.DeviceStatus("dev-1"))
	}
	if err := inst.Stop(context.Background()); err != nil {
		t.Fatalf("Instance.Stop: %v", err)
	}
	if gateway.GetAgent("dev-1") != nil {
		t.Fatal("voice agent remained attached after runtime stop")
	}
}

func TestVoiceGatewaySimulateCallUsesProductionAdapterAndRTP(t *testing.T) {
	mediaConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer mediaConn.Close()
	requests := make(chan string, 16)
	registrar := startVoiceAdapterRegistrar(t, mediaConn.LocalAddr().(*net.UDPAddr).Port, requests)
	svc := newTestServiceWithRegistrar(t, registrar)
	gateway := voicehost.NewGateway()
	if err := gateway.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer gateway.Stop()
	inst := &Instance{}
	inst.setService(newServiceAdapter(svc))
	if err := attachVoiceAgent(StartRequest{DeviceID: "dev-1", VoiceGateway: gateway}, inst, newServiceAdapter(svc)); err != nil {
		t.Fatal(err)
	}
	defer inst.Stop(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := gateway.SimulateCall(ctx, "dev-1", voicehost.SimulateCallRequest{
		Callee: "+8613800000000", HoldSeconds: 1,
	})
	if err != nil || !result.Success {
		t.Fatalf("SimulateCall result=%+v err=%v", result, err)
	}
	assertProductionVoiceRTP(t, mediaConn)
	assertProductionVoiceRequests(t, requests)
	adapter := gateway.GetAgent("dev-1").(*voiceAgentAdapter)
	if adapter.agent.IsBusy() || adapter.agent.Snapshot().ActiveCall != nil {
		t.Fatalf("call remained active after timed BYE: %+v", adapter.agent.Snapshot())
	}
}

func TestVoiceAgentAdapterExposesMediaControls(t *testing.T) {
	var adapter interface{} = &voiceAgentAdapter{agent: voice.NewAgent("dev-1", nil, nil)}
	if _, ok := adapter.(interface{ SendDTMF(string, string) error }); !ok {
		t.Fatal("voice adapter does not expose DTMF")
	}
	if _, ok := adapter.(interface{ StartPCAP(string) error }); !ok {
		t.Fatal("voice adapter does not expose PCAP start")
	}
	if _, ok := adapter.(interface{ StopPCAP() error }); !ok {
		t.Fatal("voice adapter does not expose PCAP stop")
	}
}

func startVoiceAdapterRegistrar(t *testing.T, mediaPort int, requests chan<- string) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buffer := make([]byte, 64*1024)
		for {
			n, remote, readErr := conn.ReadFromUDP(buffer)
			if readErr != nil {
				return
			}
			request := string(buffer[:n])
			select {
			case requests <- request:
			default:
			}
			if strings.HasPrefix(request, "ACK ") {
				continue
			}
			body, extra := "", ""
			if strings.HasPrefix(request, "REGISTER ") {
				extra = "P-Associated-URI: <sip:+15551234567@ims.example.com>\r\n"
			}
			if strings.HasPrefix(request, "INVITE ") {
				body = fmt.Sprintf("v=0\r\no=- 2 2 IN IP4 127.0.0.1\r\ns=ims\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP 0\r\n", mediaPort)
				extra = "To: <sip:callee@ims.example.com>;tag=voice-remote\r\nContact: <sip:callee@ims.example.com>\r\nContent-Type: application/sdp\r\n"
			}
			response := fmt.Sprintf("SIP/2.0 200 OK\r\nVia: %s\r\nCall-ID: %s\r\nCSeq: %s\r\n%sContent-Length: %d\r\n\r\n%s",
				testSIPHeader(request, "Via"), testSIPHeader(request, "Call-ID"), testSIPHeader(request, "CSeq"), extra, len(body), body)
			_, _ = conn.WriteToUDP([]byte(response), remote)
		}
	}()
	return conn
}

func assertProductionVoiceRTP(t *testing.T, mediaConn *net.UDPConn) {
	t.Helper()
	if err := mediaConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, 256)
	n, _, err := mediaConn.ReadFromUDP(packet)
	if err != nil {
		t.Fatalf("read production adapter RTP: %v", err)
	}
	if n != 172 || packet[1]&0x7f != 0 {
		t.Fatalf("production adapter RTP n=%d pt=%d", n, packet[1]&0x7f)
	}
}

func assertProductionVoiceRequests(t *testing.T, requests <-chan string) {
	t.Helper()
	seen := map[string]bool{}
	for {
		select {
		case request := <-requests:
			method := strings.Fields(request)[0]
			seen[method] = true
			if method == "INVITE" && (strings.Contains(request, "m=audio 0 ") || !strings.Contains(request, "RTP/AVP 104 114 9 8 0 101")) {
				t.Fatalf("INVITE did not advertise the allocated production media endpoint: %q", request)
			}
		default:
			if !seen["INVITE"] || !seen["ACK"] || !seen["BYE"] {
				t.Fatalf("voice requests = %+v, want INVITE ACK BYE", seen)
			}
			return
		}
	}
}

// memDeliveryStore is an in-memory delivery store for adapter tests.
type memDeliveryStore struct {
	status        *messaging.DeliveryStatus
	sipCode       int
	inboundStored bool
	degraded      bool
}

type fragmentOnlyDeliveryStore struct {
	messaging.DeliveryStore
	fragments messaging.InboundFragmentStore
}

func (store fragmentOnlyDeliveryStore) LoadInboundFragments(
	owner messaging.InboundFragmentOwner,
) ([]messaging.StoredInboundFragment, error) {
	return store.fragments.LoadInboundFragments(owner)
}

func (store fragmentOnlyDeliveryStore) SaveInboundFragment(
	scope messaging.InboundFragmentScope,
	fragment messaging.InboundFragment,
) (messaging.InboundFragmentSaveResult, error) {
	return store.fragments.SaveInboundFragment(scope, fragment)
}

func (store fragmentOnlyDeliveryStore) DeleteInboundFragments(
	scope messaging.InboundFragmentScope,
) error {
	return store.fragments.DeleteInboundFragments(scope)
}

func (store fragmentOnlyDeliveryStore) MarkInboundFragmentAcked(
	scope messaging.InboundFragmentScope,
	sequence int,
	at time.Time,
) error {
	return store.fragments.MarkInboundFragmentAcked(scope, sequence, at)
}

func (m *memDeliveryStore) CreateSMSDelivery(messageID, imsi, deviceID, peer, content string, partsTotal int, at time.Time) error {
	m.status = &messaging.DeliveryStatus{MessageID: messageID, IMSI: imsi, DeviceID: deviceID, Peer: peer, Content: content, PartsTotal: partsTotal, State: "accepted"}
	return nil
}
func (m *memDeliveryStore) UpsertSMSDeliveryPart(messageID string, partNo int, callID string, rpMR int, state string, sentAt time.Time) error {
	return nil
}
func (m *memDeliveryStore) MarkSMSDeliveryPartSIPResult(messageID string, partNo, sipCode int, state, errText string, at time.Time) error {
	m.sipCode = sipCode
	return nil
}
func (m *memDeliveryStore) MarkSMSDeliveryPartReport(inReplyTo, callID, deviceID string, rpMR int, state string, sipCode int, rpCause int, errText string, at time.Time) (messaging.DeliveryPartMatch, error) {
	return messaging.DeliveryPartMatch{MessageID: inReplyTo, PartNo: rpMR, State: state, Matched: true}, nil
}
func (m *memDeliveryStore) RecomputeSMSDelivery(messageID string, at time.Time) error { return nil }
func (m *memDeliveryStore) UpdateSMSDeliveryState(messageID, state, lastError string, acks int, at time.Time) error {
	return nil
}
func (m *memDeliveryStore) GetSMSDeliveryStatus(messageID string) (*messaging.DeliveryStatus, error) {
	if m.status == nil {
		return nil, messaging.ErrDeliveryNotFound
	}
	return m.status, nil
}
func (m *memDeliveryStore) LoadInboundFragments(messaging.InboundFragmentOwner) ([]messaging.StoredInboundFragment, error) {
	return nil, nil
}
func (m *memDeliveryStore) SaveInboundFragment(
	messaging.InboundFragmentScope,
	messaging.InboundFragment,
) (messaging.InboundFragmentSaveResult, error) {
	m.inboundStored = true
	return messaging.InboundFragmentSaveResult{Inserted: true}, nil
}
func (m *memDeliveryStore) DeleteInboundFragments(messaging.InboundFragmentScope) error { return nil }
func (m *memDeliveryStore) MarkInboundFragmentAcked(
	messaging.InboundFragmentScope,
	int,
	time.Time,
) error {
	return nil
}
func (m *memDeliveryStore) MarkInboundFragmentsDegraded(
	messaging.InboundFragmentScope,
	time.Time,
) error {
	m.degraded = true
	return nil
}

func TestDeliveryStoreAdapter(t *testing.T) {
	store := &memDeliveryStore{}
	adapter := newDeliveryStoreAdapter(store)
	if err := adapter.CreateSMSDelivery("msg-1", "310260123456789", "dev-1", "+8613800000000", "hi", 1, time.Now()); err != nil {
		t.Fatalf("CreateSMSDelivery: %v", err)
	}
	st, err := adapter.GetSMSDeliveryStatus("msg-1")
	if err != nil {
		t.Fatalf("GetSMSDeliveryStatus: %v", err)
	}
	if st.MessageID != "msg-1" || st.State != "accepted" {
		t.Errorf("status = %+v", st)
	}
	sipResults, ok := adapter.(imscore.SMSDeliverySIPResultStore)
	if !ok {
		t.Fatal("adapter did not preserve SIP result capability")
	}
	if err := sipResults.MarkSMSDeliveryPartSIPResult("msg-1", 1, 202, "pending", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if store.sipCode != 202 {
		t.Fatalf("persisted SIP code = %d", store.sipCode)
	}
	fragments, ok := adapter.(imscore.SMSInboundFragmentStore)
	if !ok {
		t.Fatal("adapter did not preserve inbound fragment capability")
	}
	if _, err := fragments.SaveInboundFragment(
		messaging.InboundFragmentScope{}, messaging.InboundFragment{},
	); err != nil || !store.inboundStored {
		t.Fatalf("SaveInboundFragment err=%v stored=%v", err, store.inboundStored)
	}
	lifecycle, ok := adapter.(imscore.SMSInboundFragmentLifecycleStore)
	if !ok {
		t.Fatal("adapter did not preserve inbound fragment lifecycle capability")
	}
	if err := lifecycle.MarkInboundFragmentsDegraded(messaging.InboundFragmentScope{}, time.Now()); err != nil || !store.degraded {
		t.Fatalf("MarkInboundFragmentsDegraded err=%v degraded=%v", err, store.degraded)
	}
}

func TestRuntimeCoreDeliveryStorePreservesOptionalSIPResults(t *testing.T) {
	store := &memDeliveryStore{}
	adapter := runtimeCoreDeliveryStore(store)
	sipResults, ok := adapter.(smsdelivery.SIPResultStore)
	if !ok {
		t.Fatal("runtimehost runtimecore adapter lost SIP result capability")
	}
	if err := sipResults.MarkSMSDeliveryPartSIPResult(
		"msg-1", 1, 202, "pending", "", time.Now(),
	); err != nil {
		t.Fatalf("MarkSMSDeliveryPartSIPResult: %v", err)
	}
	if store.sipCode != 202 {
		t.Fatalf("persisted SIP code = %d", store.sipCode)
	}
	fragments, ok := adapter.(smsdelivery.InboundFragmentStore)
	if !ok {
		t.Fatal("runtimehost runtimecore adapter lost inbound fragment capability")
	}
	if _, err := fragments.SaveInboundFragment(
		smsdelivery.InboundFragmentScope{}, smsdelivery.InboundFragment{},
	); err != nil || !store.inboundStored {
		t.Fatalf("SaveInboundFragment err=%v stored=%v", err, store.inboundStored)
	}
	lifecycle, ok := adapter.(smsdelivery.InboundFragmentLifecycleStore)
	if !ok {
		t.Fatal("runtimehost runtimecore adapter lost fragment lifecycle capability")
	}
	if err := lifecycle.MarkInboundFragmentsDegraded(smsdelivery.InboundFragmentScope{}, time.Now()); err != nil || !store.degraded {
		t.Fatalf("MarkInboundFragmentsDegraded err=%v degraded=%v", err, store.degraded)
	}
}

func TestDeliveryStoreAdaptersDoNotInventFragmentLifecycleCapability(t *testing.T) {
	underlying := &memDeliveryStore{}
	store := fragmentOnlyDeliveryStore{DeliveryStore: underlying, fragments: underlying}
	adapters := []any{newDeliveryStoreAdapter(store), runtimeCoreDeliveryStore(store)}
	for _, adapter := range adapters {
		if _, ok := adapter.(smsdelivery.InboundFragmentStore); !ok {
			t.Fatalf("adapter %T lost fragment capability", adapter)
		}
		if _, ok := adapter.(smsdelivery.InboundFragmentLifecycleStore); ok {
			t.Fatalf("adapter %T invented lifecycle capability", adapter)
		}
	}
}

func TestRuntimeCoreDeliveryStorePreservesMatchedReport(t *testing.T) {
	store := &memDeliveryStore{}
	match, err := runtimeCoreDeliveryStore(store).MarkSMSDeliveryPartReport(
		"msg-1", "call-1", "dev-1", 17, "acked", 200, 0, "", time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !match.Matched || match.MessageID != "msg-1" || match.PartNo != 17 || match.State != "acked" {
		t.Fatalf("report match = %+v", match)
	}
}

func TestDeliveryStatusConversion(t *testing.T) {
	createdAt := time.Date(2026, time.August, 10, 1, 58, 25, 0, time.UTC)
	updatedAt := createdAt.Add(300 * time.Millisecond)
	reportAt := updatedAt
	internal := &imscore.DeliveryStatus{
		MessageID:  "m1",
		IMSI:       "310260123456789",
		DeviceID:   "dev-1",
		Peer:       "+8613800000000",
		Content:    "hi",
		PartsTotal: 1,
		Acks:       1,
		State:      "delivered",
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		Parts: []imscore.DeliveryPartStatus{{
			PartNo: 1, CallID: "c1", InReplyTo: "reply-c1", RPMR: 23,
			State: "delivered", SIPCode: 200, RPCause: 0, RPCauseText: "accepted",
			ErrorText: "", SentAt: createdAt, ReportAt: &reportAt,
			CreatedAt: createdAt, UpdatedAt: updatedAt,
		}},
	}
	ext := deliveryStatusFromInternal(internal)
	if ext.MessageID != "m1" || ext.State != "delivered" || ext.CreatedAt != createdAt || ext.UpdatedAt != updatedAt {
		t.Errorf("converted = %+v", ext)
	}
	if len(ext.Parts) != 1 || ext.Parts[0].SIPCode != 200 || ext.Parts[0].RPMR != 23 ||
		ext.Parts[0].InReplyTo != "reply-c1" || ext.Parts[0].ReportAt == nil || *ext.Parts[0].ReportAt != reportAt {
		t.Errorf("parts = %+v", ext.Parts)
	}
	back := deliveryStatusToInternal(ext)
	if back.MessageID != "m1" || back.CreatedAt != createdAt || len(back.Parts) != 1 ||
		back.Parts[0].RPMR != 23 || back.Parts[0].UpdatedAt != updatedAt {
		t.Errorf("round-trip = %+v", back)
	}
}

func TestStartInstanceAsync(t *testing.T) {
	prepared := &identity.PreparedSession{
		Profile:     identity.Profile{IMSI: "310260123456789", MCC: "310", MNC: "260"},
		IMSIdentity: identity.IMSIdentity{IMPI: "310260123456789@ims.example", IMPU: "sip:310260123456789@ims.example", Domain: "ims.example"},
		EPDGAddr:    "epdg.example.com",
	}
	req := runtimeTestRequest(prepared, newLifecycleTunnel(nil))
	req.Mode = StartModeReader
	req.DeviceID = "dev-1"
	instCh, errCh := startInstanceAsync(context.Background(), req)
	select {
	case inst := <-instCh:
		if inst == nil {
			t.Fatal("nil instance")
		}
		if st := inst.State(); st.SessionState != "established" {
			t.Errorf("session state = %q", st.SessionState)
		}
	case err := <-errCh:
		t.Fatalf("start: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("start timed out")
	}
}
