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
	registrar := startTestRegistrar(t)
	cfg := &imscore.IMSConfig{
		DeviceID:    "dev-1",
		IMSI:        "310260123456789",
		IMPI:        "310260123456789@ims.example.com",
		Domain:      "ims.example.com",
		SMSC:        "+123",
		LocalIP:     net.IPv4(127, 0, 0, 1),
		Registrar:   registrar.LocalAddr().String(),
		AKAProvider: stubAKA{},
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
	if res.Code != "0" {
		t.Errorf("ussd code = %q", res.Code)
	}
}

func TestServiceAdapterNoService(t *testing.T) {
	adapter := newServiceAdapter(nil)
	if _, err := adapter.SendSMSWithResult(context.Background(), "1", "x"); !errors.Is(err, errNoService) {
		t.Errorf("err = %v, want errNoService", err)
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

// memDeliveryStore is an in-memory delivery store for adapter tests.
type memDeliveryStore struct {
	status *messaging.DeliveryStatus
}

func (m *memDeliveryStore) CreateSMSDelivery(messageID, imsi, deviceID, peer, content string, partsTotal int, at time.Time) error {
	m.status = &messaging.DeliveryStatus{MessageID: messageID, IMSI: imsi, DeviceID: deviceID, Peer: peer, Content: content, PartsTotal: partsTotal, State: "accepted"}
	return nil
}
func (m *memDeliveryStore) UpsertSMSDeliveryPart(messageID string, partNo int, callID string, rpMR int, state string, sentAt time.Time) error {
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
}

func TestDeliveryStatusConversion(t *testing.T) {
	internal := &imscore.DeliveryStatus{
		MessageID:  "m1",
		IMSI:       "310260123456789",
		DeviceID:   "dev-1",
		Peer:       "+8613800000000",
		Content:    "hi",
		PartsTotal: 1,
		Acks:       1,
		State:      "delivered",
		Parts:      []imscore.DeliveryPartStatus{{PartNo: 1, CallID: "c1", State: "delivered", SIPCode: 200}},
	}
	ext := deliveryStatusFromInternal(internal)
	if ext.MessageID != "m1" || ext.State != "delivered" {
		t.Errorf("converted = %+v", ext)
	}
	if len(ext.Parts) != 1 || ext.Parts[0].SIPCode != 200 {
		t.Errorf("parts = %+v", ext.Parts)
	}
	back := deliveryStatusToInternal(ext)
	if back.MessageID != "m1" || len(back.Parts) != 1 {
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
