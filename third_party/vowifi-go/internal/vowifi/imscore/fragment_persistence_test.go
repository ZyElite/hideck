package imscore

import (
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/smsdelivery"
)

type memoryInboundFragmentStore struct {
	*memoryDeliveryStore
	fragmentMu sync.Mutex
	fragments  map[smsInboundFragmentScope]map[int]smsInboundFragmentRecord
}

func newMemoryInboundFragmentStore() *memoryInboundFragmentStore {
	return &memoryInboundFragmentStore{
		memoryDeliveryStore: newMemoryDeliveryStore(),
		fragments:           make(map[smsInboundFragmentScope]map[int]smsInboundFragmentRecord),
	}
}

func (s *memoryInboundFragmentStore) LoadInboundFragments(
	owner smsInboundFragmentOwner,
) ([]storedSMSInboundFragment, error) {
	s.fragmentMu.Lock()
	defer s.fragmentMu.Unlock()
	var result []storedSMSInboundFragment
	for scope, fragments := range s.fragments {
		if scope.Owner != owner {
			continue
		}
		for _, fragment := range fragments {
			result = append(result, storedSMSInboundFragment{Scope: scope, Fragment: fragment})
		}
	}
	return result, nil
}

func (s *memoryInboundFragmentStore) SaveInboundFragment(
	scope smsInboundFragmentScope,
	fragment smsInboundFragmentRecord,
) (smsInboundFragmentSaveResult, error) {
	s.fragmentMu.Lock()
	defer s.fragmentMu.Unlock()
	if s.fragments[scope] == nil {
		s.fragments[scope] = make(map[int]smsInboundFragmentRecord)
	}
	result := smsInboundFragmentSaveResult{}
	if existing, found := s.fragments[scope][fragment.Sequence]; found {
		result.CollisionReason = storedFragmentCollision(existing, fragment)
		result.Fragments = fragmentMapValues(s.fragments[scope])
		if result.CollisionReason != "" {
			return result, smsdelivery.ErrInboundFragmentCollision
		}
		return result, nil
	}
	s.fragments[scope][fragment.Sequence] = fragment
	result.Inserted = true
	result.Fragments = fragmentMapValues(s.fragments[scope])
	return result, nil
}

func (s *memoryInboundFragmentStore) DeleteInboundFragments(scope smsInboundFragmentScope) error {
	s.fragmentMu.Lock()
	delete(s.fragments, scope)
	s.fragmentMu.Unlock()
	return nil
}

func (s *memoryInboundFragmentStore) MarkInboundFragmentAcked(
	scope smsInboundFragmentScope,
	sequence int,
	at time.Time,
) error {
	s.fragmentMu.Lock()
	fragment, found := s.fragments[scope][sequence]
	if found {
		fragment.AckSent, fragment.AckSentAt = true, at
		s.fragments[scope][sequence] = fragment
	}
	s.fragmentMu.Unlock()
	return nil
}

func TestInboundMultipartSMSRecoversAcrossServiceReplacement(t *testing.T) {
	store := newMemoryInboundFragmentStore()
	firstService, _ := newFragmentPersistenceService(t, store)
	first := persistedFragmentMessage(1, "first ")
	result, err := firstService.finalizeInboundSMSData(fragmentTestRequest("mt-part-1"), first, "SIP/2.0 200 OK\r\n\r\n")
	if err != nil || result.afterReply == nil {
		t.Fatalf("first fragment result=%#v err=%v", result, err)
	}
	key := inboundSMSFragmentKey(first)
	firstService.markFragmentAcked(key, 1)
	firstService.Stop()

	secondService, subscriber := newFragmentPersistenceService(t, store)
	if err := secondService.restoreInboundFragments(); err != nil {
		t.Fatal(err)
	}
	assertRestoredFragmentAck(t, secondService, key)
	second := persistedFragmentMessage(2, "part")
	if _, err := secondService.finalizeInboundSMSData(
		fragmentTestRequest("mt-part-2"), second, "SIP/2.0 200 OK\r\n\r\n",
	); err != nil {
		t.Fatal(err)
	}
	assertRecoveredSMS(t, subscriber, "first part")
	stored, err := store.LoadInboundFragments(secondService.inboundFragmentOwner())
	if err != nil || len(stored) != 0 {
		t.Fatalf("stored fragments after assembly=%#v err=%v", stored, err)
	}
	if _, err := secondService.finalizeInboundSMSData(
		fragmentTestRequest("mt-part-2-retry"), second, "SIP/2.0 200 OK\r\n\r\n",
	); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-subscriber.events:
		t.Fatalf("duplicate completion published event %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestInboundFragmentPersistenceRejectsCollision(t *testing.T) {
	store := newMemoryInboundFragmentStore()
	service, _ := newFragmentPersistenceService(t, store)
	fragment := persistedFragmentMessage(1, "first")
	if _, err := service.finalizeInboundSMSData(
		fragmentTestRequest("mt-original"), fragment, "SIP/2.0 200 OK\r\n\r\n",
	); err != nil {
		t.Fatal(err)
	}
	fragment.content = "changed"
	_, err := service.finalizeInboundSMSData(
		fragmentTestRequest("mt-collision"), fragment, "SIP/2.0 200 OK\r\n\r\n",
	)
	if err == nil || !errors.Is(err, smsdelivery.ErrInboundFragmentCollision) {
		t.Fatalf("collision error=%v", err)
	}
	stored, loadErr := store.LoadInboundFragments(service.inboundFragmentOwner())
	if loadErr != nil || len(stored) != 0 {
		t.Fatalf("stored fragments after collision=%#v err=%v", stored, loadErr)
	}
}

func TestInboundFragmentPersistenceDeletesExpiredSession(t *testing.T) {
	store := newMemoryInboundFragmentStore()
	service, subscriber := newFragmentPersistenceService(t, store)
	message := persistedFragmentMessage(1, "stale")
	if _, err := service.finalizeInboundSMSData(
		fragmentTestRequest("mt-stale"), message, "SIP/2.0 200 OK\r\n\r\n",
	); err != nil {
		t.Fatal(err)
	}
	key := inboundSMSFragmentKey(message)
	service.fragmentMu.Lock()
	service.fragmentCache[key][0].Time = time.Now().Add(-time.Second)
	service.fragmentMu.Unlock()
	if err := service.cleanupExpiredFragments(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	stored, err := store.LoadInboundFragments(service.inboundFragmentOwner())
	if err != nil || len(stored) != 0 {
		t.Fatalf("stored expired fragments=%#v err=%v", stored, err)
	}
	assertIMSEventTypes(t, subscriber, "LogNotify", "SMSReceived")
}

func newFragmentPersistenceService(
	t *testing.T,
	store *memoryInboundFragmentStore,
) (*Service, *captureIMSEventSubscriber) {
	t.Helper()
	bus := NewEventBus()
	subscriber := &captureIMSEventSubscriber{events: make(chan events.Event, 4)}
	bus.Subscribe(subscriber)
	service, err := New(&IMSConfig{
		DeviceID: "wwan0", IMSI: "234102356143376", EventBus: bus, DeliveryStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Stop)
	return service, subscriber
}

func persistedFragmentMessage(sequence int, content string) inboundSMS {
	return inboundSMS{
		sender: "giffgaff", serviceCenter: "+447802000332",
		targetURI: "sip:+447840844894@ims.example", content: content,
		timestamp: time.Date(2026, time.August, 10, 7, 4, 36, 0, time.UTC),
		rpMR:      byte(60 + sequence), concatRef: 198, refBits: 8, total: 2, partNo: sequence,
	}
}

func fragmentTestRequest(callID string) string {
	return "MESSAGE sip:user@ims.example SIP/2.0\r\n" +
		"Via: SIP/2.0/TCP 10.0.0.1:5060;branch=z9hG4bK-" + callID + "\r\n" +
		"From: <sip:giffgaff@ims.example>;tag=from-tag\r\n" +
		"To: <sip:+447840844894@ims.example>\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 MESSAGE\r\nContent-Length: 0\r\n\r\n"
}

func assertRestoredFragmentAck(t *testing.T, service *Service, key string) {
	t.Helper()
	service.fragmentMu.Lock()
	fragments := append([]*smsFragment(nil), service.fragmentCache[key]...)
	service.fragmentMu.Unlock()
	if len(fragments) != 1 || !fragments[0].AckSent || fragments[0].Seq != 1 {
		t.Fatalf("restored fragments=%#v", fragments)
	}
}

func assertRecoveredSMS(t *testing.T, subscriber *captureIMSEventSubscriber, content string) {
	t.Helper()
	select {
	case event := <-subscriber.events:
		received, ok := event.(*events.EventSMSReceived)
		if !ok || received.Content != content || received.Sender != "giffgaff" {
			t.Fatalf("recovered event=%#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("recovered multipart SMS was not published")
	}
}

func storedFragmentCollision(existing, candidate smsInboundFragmentRecord) string {
	if existing.Total != candidate.Total {
		return "total_mismatch"
	}
	if existing.Content != candidate.Content {
		return "sequence_content_mismatch"
	}
	return ""
}

func fragmentMapValues(values map[int]smsInboundFragmentRecord) []smsInboundFragmentRecord {
	result := make([]smsInboundFragmentRecord, 0, len(values))
	for _, fragment := range values {
		result = append(result, fragment)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Sequence < result[right].Sequence
	})
	return result
}
