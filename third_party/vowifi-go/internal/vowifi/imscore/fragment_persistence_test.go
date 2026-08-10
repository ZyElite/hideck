package imscore

import (
	"errors"
	"sort"
	"strings"
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

func (s *memoryInboundFragmentStore) MarkInboundFragmentsDegraded(
	scope smsInboundFragmentScope,
	at time.Time,
) error {
	s.fragmentMu.Lock()
	defer s.fragmentMu.Unlock()
	if len(s.fragments[scope]) == 0 {
		return errors.New("fragment session not found")
	}
	for sequence, fragment := range s.fragments[scope] {
		fragment.DegradedAt = at
		s.fragments[scope][sequence] = fragment
	}
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
	firstService.StopCurrent()

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
	stored, err = store.LoadInboundFragments(secondService.inboundFragmentOwner())
	if err != nil || len(stored) != 0 {
		t.Fatalf("duplicate completion left fragments=%#v err=%v", stored, err)
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

func TestInboundFragmentPersistenceRetainsDegradedSessionForLateCompletion(t *testing.T) {
	store := newMemoryInboundFragmentStore()
	service, subscriber := newFragmentPersistenceService(t, store)
	message := persistedFragmentMessage(1, "stale")
	if _, err := service.finalizeInboundSMSData(
		fragmentTestRequest("mt-stale"), message, "SIP/2.0 200 OK\r\n\r\n",
	); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := service.cleanupExpiredFragments(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	stored, err := store.LoadInboundFragments(service.inboundFragmentOwner())
	if err != nil || len(stored) != 1 || stored[0].Fragment.DegradedAt.IsZero() {
		t.Fatalf("stored degraded fragments=%#v err=%v", stored, err)
	}
	assertNoIMSEvent(t, subscriber, "incomplete fragment timeout")
	if _, err := service.finalizeInboundSMSData(
		fragmentTestRequest("mt-stale-duplicate"), message, "SIP/2.0 200 OK\r\n\r\n",
	); err != nil {
		t.Fatal(err)
	}
	if err := service.cleanupExpiredFragments(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	assertNoIMSEvent(t, subscriber, "duplicate degraded notification")
	service.StopCurrent()

	restored, restoredSubscriber := newFragmentPersistenceService(t, store)
	if err := restored.restoreInboundFragments(); err != nil {
		t.Fatal(err)
	}
	assertNoIMSEvent(t, restoredSubscriber, "degraded notification after restore")
	second := persistedFragmentMessage(2, " complete")
	if _, err := restored.finalizeInboundSMSData(
		fragmentTestRequest("mt-late-part"), second, "SIP/2.0 200 OK\r\n\r\n",
	); err != nil {
		t.Fatal(err)
	}
	assertFragmentSMS(t, restoredSubscriber, expectedFragmentSMS{
		Content: "stale complete",
	})
	stored, err = store.LoadInboundFragments(restored.inboundFragmentOwner())
	if err != nil || len(stored) != 0 {
		t.Fatalf("stored fragments after late completion=%#v err=%v", stored, err)
	}
}

type expectedFragmentSMS struct {
	Content    string
	SessionKey string
	Incomplete bool
}

func assertFragmentSMS(
	t *testing.T,
	subscriber *captureIMSEventSubscriber,
	expected expectedFragmentSMS,
) string {
	t.Helper()
	select {
	case event := <-subscriber.events:
		received, ok := event.(*events.EventSMSReceived)
		if !ok || received.Content != expected.Content || received.Sender != "giffgaff" ||
			received.Incomplete != expected.Incomplete {
			t.Fatalf("fragment SMS event=%#v", event)
		}
		if expected.SessionKey == "" && received.FragmentSessionKey == "" {
			t.Fatalf("fragment SMS event has no session ID: %#v", event)
		}
		if expected.SessionKey != "" && expected.SessionKey != received.FragmentSessionKey {
			t.Fatalf("fragment session ID=%q want %q", received.FragmentSessionKey, expected.SessionKey)
		}
		return received.FragmentSessionKey
	case <-time.After(time.Second):
		t.Fatal("fragment SMS event was not published")
	}
	return ""
}

func TestFragmentSessionInstanceIDSeparatesReusedReferences(t *testing.T) {
	key := "sender=giffgaff|ref=7|bits=8"
	first := []*smsFragment{
		{Seq: 2, Time: time.Unix(200, 0)},
		{Seq: 1, Time: time.Unix(100, 0)},
	}
	reordered := []*smsFragment{first[1], first[0]}
	reused := []*smsFragment{{Seq: 1, Time: time.Unix(300, 0)}}
	firstID := buildFragmentSessionInstanceID(key, first)
	if firstID == "" || firstID != buildFragmentSessionInstanceID(key, reordered) {
		t.Fatalf("session ID is empty or order-dependent: %q", firstID)
	}
	if firstID == buildFragmentSessionInstanceID(key, reused) {
		t.Fatal("reused concatenation reference shared a session ID")
	}
}

func TestInboundFragmentPersistenceDeletesAtLateWindowExpiry(t *testing.T) {
	store := newMemoryInboundFragmentStore()
	service, subscriber := newFragmentPersistenceService(t, store)
	message := persistedFragmentMessage(1, "stale")
	if _, err := service.finalizeInboundSMSData(
		fragmentTestRequest("mt-expiry"), message, "SIP/2.0 200 OK\r\n\r\n",
	); err != nil {
		t.Fatal(err)
	}
	key := inboundSMSFragmentKey(message)
	setRuntimeFragmentTimes(t, service, key, time.Now().Add(-time.Second), time.Time{})
	if err := service.cleanupExpiredFragments(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	assertNoIMSEvent(t, subscriber, "incomplete fragment timeout")
	expiredAt := time.Now().Add(-inboundSMSLateReassemblyTTL - time.Second)
	if err := store.MarkInboundFragmentsDegraded(service.inboundFragmentScope(key), expiredAt); err != nil {
		t.Fatal(err)
	}
	setRuntimeFragmentTimes(t, service, key, time.Time{}, expiredAt)
	if err := service.cleanupExpiredFragments(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	stored, err := store.LoadInboundFragments(service.inboundFragmentOwner())
	if err != nil || len(stored) != 0 {
		t.Fatalf("stored fragments after late expiry=%#v err=%v", stored, err)
	}
	assertNoIMSEvent(t, subscriber, "notification at final expiry")
}

func TestInboundFragmentPersistenceRejectsArrivalAfterLateWindow(t *testing.T) {
	store := newMemoryInboundFragmentStore()
	service, subscriber := newFragmentPersistenceService(t, store)
	first := persistedFragmentMessage(1, "stale")
	if _, err := service.finalizeInboundSMSData(
		fragmentTestRequest("mt-arrival-expiry"), first, "SIP/2.0 200 OK\r\n\r\n",
	); err != nil {
		t.Fatal(err)
	}
	key := inboundSMSFragmentKey(first)
	setRuntimeFragmentTimes(t, service, key, time.Now().Add(-time.Second), time.Time{})
	if err := service.cleanupExpiredFragments(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	assertNoIMSEvent(t, subscriber, "incomplete fragment timeout")
	expiredAt := time.Now().Add(-inboundSMSLateReassemblyTTL - time.Second)
	if err := store.MarkInboundFragmentsDegraded(service.inboundFragmentScope(key), expiredAt); err != nil {
		t.Fatal(err)
	}
	setRuntimeFragmentTimes(t, service, key, time.Time{}, expiredAt)
	second := persistedFragmentMessage(2, " complete")
	_, err := service.finalizeInboundSMSData(
		fragmentTestRequest("mt-too-late"), second, "SIP/2.0 200 OK\r\n\r\n",
	)
	if err == nil || !strings.Contains(err.Error(), "after reassembly window") {
		t.Fatalf("late fragment error=%v", err)
	}
	stored, loadErr := store.LoadInboundFragments(service.inboundFragmentOwner())
	if loadErr != nil || len(stored) != 0 {
		t.Fatalf("stored fragments after rejected late arrival=%#v err=%v", stored, loadErr)
	}
}

func setRuntimeFragmentTimes(
	t *testing.T,
	service *Service,
	key string,
	arrivedAt, degradedAt time.Time,
) {
	t.Helper()
	service.fragmentMu.Lock()
	defer service.fragmentMu.Unlock()
	for _, fragment := range service.fragmentCache[key] {
		if !arrivedAt.IsZero() {
			fragment.Time = arrivedAt
		}
		if !degradedAt.IsZero() {
			fragment.DegradedAt = degradedAt
		}
	}
	if len(service.fragmentCache[key]) == 0 {
		t.Fatalf("fragment session %q not found", key)
	}
}

func assertNoIMSEvent(t *testing.T, subscriber *captureIMSEventSubscriber, context string) {
	t.Helper()
	select {
	case event := <-subscriber.events:
		t.Fatalf("%s: unexpected event %#v", context, event)
	case <-time.After(20 * time.Millisecond):
	}
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
	t.Cleanup(service.StopCurrent)
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
