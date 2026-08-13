package phone

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
	"github.com/pion/webrtc/v4"
)

type fakeVoiceGateway struct {
	mu              sync.Mutex
	incoming        func(voicehost.IncomingCall)
	events          func(voicehost.CallEvent)
	unsubscribed    int
	beginSnapshot   voicehost.CallSnapshot
	beginEvents     []voicehost.CallEvent
	rejectEmits     bool
	hangupCalls     chan string
	dtmfCalls       chan string
	rejectCalls     chan voicehost.RejectRequest
	captureError    error
	activeSnapshots map[string]voicehost.CallSnapshot
}

func newFakeVoiceGateway() *fakeVoiceGateway {
	return &fakeVoiceGateway{
		beginSnapshot: voicehost.CallSnapshot{CallID: "outbound-1", DeviceID: "dev-1"},
		hangupCalls:   make(chan string, 8), dtmfCalls: make(chan string, 8),
		rejectCalls:     make(chan voicehost.RejectRequest, 8),
		activeSnapshots: make(map[string]voicehost.CallSnapshot),
	}
}

func (g *fakeVoiceGateway) SubscribeIncomingCalls(handler func(voicehost.IncomingCall)) func() {
	g.mu.Lock()
	g.incoming = handler
	g.mu.Unlock()
	return g.unsubscribe
}

func (g *fakeVoiceGateway) SubscribeCallEvents(handler func(voicehost.CallEvent)) func() {
	g.mu.Lock()
	g.events = handler
	g.mu.Unlock()
	return g.unsubscribe
}

func (g *fakeVoiceGateway) BeginCall(_ context.Context, request voicehost.BeginCallRequest) (voicehost.CallSnapshot, error) {
	g.mu.Lock()
	handler, pending, snapshot := g.events, append([]voicehost.CallEvent(nil), g.beginEvents...), g.beginSnapshot
	g.mu.Unlock()
	for _, event := range pending {
		handler(event)
	}
	snapshot.DeviceID = request.DeviceID
	return snapshot, nil
}

func (g *fakeVoiceGateway) ActiveCall(deviceID string) *voicehost.CallSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	snapshot, ok := g.activeSnapshots[deviceID]
	if !ok {
		return nil
	}
	return &snapshot
}

func (g *fakeVoiceGateway) AnswerIncomingCall(_ context.Context, request voicehost.AnswerRequest) (voicehost.AnswerResult, error) {
	return voicehost.AnswerResult{CallID: request.CallID, State: "Connected"}, nil
}

func (g *fakeVoiceGateway) RejectIncomingCall(request voicehost.RejectRequest) error {
	g.rejectCalls <- request
	if g.rejectEmits {
		g.emitEvent(voicehost.CallEvent{
			Type: "CallCanceled", DeviceID: request.DeviceID, CallID: request.CallID,
			Reason: "local_reject", Time: time.Now(),
		})
	}
	return nil
}

func (g *fakeVoiceGateway) HangupCall(_ context.Context, deviceID, callID string) error {
	g.hangupCalls <- callID
	g.emitEvent(voicehost.CallEvent{
		Type: "CallEnded", DeviceID: deviceID, CallID: callID,
		Reason: "local_hangup", Time: time.Now(),
	})
	return nil
}

func (g *fakeVoiceGateway) SendCallDTMF(_, callID, digit string) error {
	g.dtmfCalls <- callID + ":" + digit
	return nil
}

func (g *fakeVoiceGateway) StartCallCapture(_, _, _ string) error { return g.captureError }

func (g *fakeVoiceGateway) emitIncoming(call voicehost.IncomingCall) {
	g.mu.Lock()
	handler := g.incoming
	g.mu.Unlock()
	handler(call)
}

func (g *fakeVoiceGateway) emitEvent(event voicehost.CallEvent) {
	g.mu.Lock()
	handler := g.events
	g.mu.Unlock()
	if handler != nil {
		handler(event)
	}
}

func (g *fakeVoiceGateway) unsubscribe() {
	g.mu.Lock()
	g.unsubscribed++
	g.mu.Unlock()
}

type memoryCallStore struct {
	mu      sync.Mutex
	records map[string]CallRecord
}

func newMemoryCallStore() *memoryCallStore {
	return &memoryCallStore{records: make(map[string]CallRecord)}
}

func (s *memoryCallStore) Upsert(_ context.Context, record CallRecord) error {
	s.mu.Lock()
	s.records[record.CallID] = record
	s.mu.Unlock()
	return nil
}

func (s *memoryCallStore) List(_ context.Context, _ int) ([]CallRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]CallRecord, 0, len(s.records))
	for _, record := range s.records {
		result = append(result, record)
	}
	return result, nil
}

func (s *memoryCallStore) record(callID string) CallRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.records[callID]
}

type capturedNotification struct{ status string }

type captureNotifier struct{ notifications chan capturedNotification }

func (n captureNotifier) NotifyCallResult(_, _, _, status, _ string, _ time.Time) {
	n.notifications <- capturedNotification{status: status}
}

func newPhoneTestService(t *testing.T, gateway *fakeVoiceGateway, store *memoryCallStore, grace time.Duration) *Service {
	t.Helper()
	service, err := NewService(ServiceOptions{
		Gateway: gateway, Store: store, WebRTCUDPAddress: "127.0.0.1:0", RecoveryGrace: grace,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Close(ctx); err != nil {
			t.Errorf("close phone service: %v", err)
		}
	})
	return service
}

func addStubMedia(t *testing.T, service *Service, id, owner, lease string) {
	t.Helper()
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	service.media.mu.Lock()
	service.media.sessions[id] = &MediaSession{
		ID: id, Owner: owner, Lease: lease, peer: peer, rtpConn: connection, closed: make(chan struct{}),
	}
	service.media.mu.Unlock()
}

func waitForRecordStatus(t *testing.T, store *memoryCallStore, callID, status string) CallRecord {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		record := store.record(callID)
		if record.Status == status {
			return record
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("call %s did not reach status %s; got %+v", callID, status, store.record(callID))
	return CallRecord{}
}
