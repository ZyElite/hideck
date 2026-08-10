package imscore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/smsdelivery"
)

type persistedFragmentContext struct {
	fragmentHandlingContext
	Store SMSInboundFragmentStore
}

func (s *Service) inboundFragmentStore() SMSInboundFragmentStore {
	if s == nil || s.delivery == nil {
		return nil
	}
	store, _ := s.delivery.(SMSInboundFragmentStore)
	return store
}

func (s *Service) inboundFragmentLifecycleStore() SMSInboundFragmentLifecycleStore {
	if s == nil || s.delivery == nil || s.inboundFragmentStore() == nil {
		return nil
	}
	store, _ := s.delivery.(SMSInboundFragmentLifecycleStore)
	return store
}

func (s *Service) inboundFragmentOwner() smsInboundFragmentOwner {
	owner := smsInboundFragmentOwner{}
	if s != nil && s.cfg != nil {
		owner.DeviceID = strings.TrimSpace(s.cfg.DeviceID)
		owner.IMSI = strings.TrimSpace(s.cfg.IMSI)
	}
	return owner
}

func (s *Service) inboundFragmentScope(key string) smsInboundFragmentScope {
	return smsInboundFragmentScope{
		Owner: s.inboundFragmentOwner(), SessionKey: strings.TrimSpace(key),
	}
}

func (s *Service) handlePersistedSMSFragment(ctx persistedFragmentContext) (fragmentAssemblyResult, error) {
	s.fragmentPersistMu.Lock()
	defer s.fragmentPersistMu.Unlock()
	scope := s.inboundFragmentScope(ctx.Key)
	result, err := ctx.Store.SaveInboundFragment(scope, storedFragmentFromRuntime(ctx.Fragment))
	if err != nil {
		return s.handleFragmentStoreError(ctx, result, err)
	}
	fragments := runtimeFragmentsFromStored(result.Fragments)
	if reason, collision := detectFragmentKeyCollision(fragments, ctx.Fragment); collision {
		result.CollisionReason = reason
		return s.handleFragmentStoreError(ctx, result, smsdelivery.ErrInboundFragmentCollision)
	}
	if fragmentLateWindowExpired(fragments, time.Now()) {
		deleteErr := ctx.Store.DeleteInboundFragments(scope)
		s.fragmentMu.Lock()
		delete(s.fragmentCache, ctx.Key)
		s.fragmentRecentExpired[ctx.Key] = time.Now()
		s.fragmentOrphanLate++
		s.fragmentMu.Unlock()
		return fragmentAssemblyResult{}, errors.Join(
			errors.New("imscore: late SMS fragment after reassembly window"), deleteErr,
		)
	}
	return s.acceptPersistedFragment(ctx, fragments, result.Inserted)
}

func (s *Service) acceptPersistedFragment(
	ctx persistedFragmentContext,
	fragments []*smsFragment,
	inserted bool,
) (fragmentAssemblyResult, error) {
	s.fragmentMu.Lock()
	if len(fragments) == 1 && ctx.Fragment.Seq != 1 && inserted {
		s.logOutOfOrderFirst(ctx.Sender, ctx.Fragment)
	}
	if !inserted {
		s.fragmentDup++
	}
	s.fragmentCache[ctx.Key] = fragments
	complete := len(fragments) == ctx.Fragment.Total && len(missingSMSSeqs(fragments, ctx.Fragment.Total)) == 0
	s.fragmentMu.Unlock()
	if !complete {
		return fragmentAssemblyResult{}, nil
	}
	if err := ctx.Store.DeleteInboundFragments(s.inboundFragmentScope(ctx.Key)); err != nil {
		return fragmentAssemblyResult{}, fmt.Errorf("imscore: delete completed SMS fragments: %w", err)
	}
	content := assembleFragmentText(fragments)
	sessionID := buildFragmentSessionInstanceID(ctx.Key, fragments)
	s.fragmentMu.Lock()
	s.recordCompletedFragmentsLocked(ctx.Key, fragments)
	delete(s.fragmentCache, ctx.Key)
	s.fragmentAssembledOK++
	s.fragmentMu.Unlock()
	return fragmentAssemblyResult{Content: content, SessionID: sessionID, Complete: true}, nil
}

func (s *Service) handleFragmentStoreError(
	ctx persistedFragmentContext,
	result smsInboundFragmentSaveResult,
	storeErr error,
) (fragmentAssemblyResult, error) {
	if !errors.Is(storeErr, smsdelivery.ErrInboundFragmentCollision) {
		return fragmentAssemblyResult{}, fmt.Errorf("imscore: persist inbound SMS fragment: %w", storeErr)
	}
	reason := strings.TrimSpace(result.CollisionReason)
	if reason == "" {
		reason = "sequence_content_mismatch"
	}
	fragments := runtimeFragmentsFromStored(result.Fragments)
	deleteErr := ctx.Store.DeleteInboundFragments(s.inboundFragmentScope(ctx.Key))
	s.fragmentMu.Lock()
	s.recordFragmentCollisionLocked(ctx.fragmentHandlingContext, reason, fragments)
	s.fragmentMu.Unlock()
	return fragmentAssemblyResult{}, errors.Join(
		fmt.Errorf("imscore: SMS fragment key collision: %s", reason), storeErr, deleteErr,
	)
}

func (s *Service) restoreInboundFragments() error {
	store := s.inboundFragmentStore()
	if store == nil {
		return nil
	}
	s.fragmentPersistMu.Lock()
	records, err := store.LoadInboundFragments(s.inboundFragmentOwner())
	if err != nil {
		s.fragmentPersistMu.Unlock()
		return fmt.Errorf("imscore: load inbound SMS fragments: %w", err)
	}
	err = s.installStoredFragments(store, records)
	s.fragmentPersistMu.Unlock()
	if err != nil {
		return err
	}
	return s.cleanupExpiredFragments(inboundSMSFragmentTTL)
}

func (s *Service) installStoredFragments(
	store SMSInboundFragmentStore,
	records []storedSMSInboundFragment,
) error {
	groups := make(map[string][]*smsFragment)
	for _, record := range records {
		if record.Scope.Owner != s.inboundFragmentOwner() {
			return errors.New("imscore: inbound SMS fragment owner mismatch")
		}
		fragment := runtimeFragmentFromStored(record.Fragment)
		if err := validateStoredFragment(record.Scope, fragment); err != nil {
			return err
		}
		if reason, collision := detectFragmentKeyCollision(groups[record.Scope.SessionKey], fragment); collision {
			return fmt.Errorf("imscore: stored SMS fragment collision: %s", reason)
		}
		groups[record.Scope.SessionKey] = append(groups[record.Scope.SessionKey], fragment)
	}
	for key, fragments := range groups {
		if err := s.installStoredFragmentGroup(store, key, fragments); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) installStoredFragmentGroup(
	store SMSInboundFragmentStore,
	key string,
	fragments []*smsFragment,
) error {
	total := fragmentTotal(fragments)
	complete := len(fragments) == total && len(missingSMSSeqs(fragments, total)) == 0
	if !complete {
		s.fragmentMu.Lock()
		s.fragmentCache[key] = fragments
		s.fragmentMu.Unlock()
		return nil
	}
	if err := store.DeleteInboundFragments(s.inboundFragmentScope(key)); err != nil {
		return fmt.Errorf("imscore: delete restored complete SMS fragments: %w", err)
	}
	first := firstSMSFragment(fragments)
	s.fragmentMu.Lock()
	s.recordCompletedFragmentsLocked(key, fragments)
	s.fragmentMu.Unlock()
	if first != nil {
		s.publishInboundSMSWithFragment(inboundSMS{
			sender: senderFromFragmentKey(key), targetURI: first.ToURI,
			content: assembleFragmentText(fragments), timestamp: latestFragmentTime(fragments),
		}, buildFragmentSessionInstanceID(key, fragments), false)
	}
	return nil
}

func validateStoredFragment(scope smsInboundFragmentScope, fragment *smsFragment) error {
	if strings.TrimSpace(scope.SessionKey) == "" || fragment == nil {
		return errors.New("imscore: invalid stored SMS fragment")
	}
	if fragment.Total < 2 || fragment.Seq < 1 || fragment.Seq > fragment.Total || fragment.Time.IsZero() {
		return errors.New("imscore: invalid stored SMS fragment bounds")
	}
	return nil
}

func (s *Service) deletePersistedFragments(key string) error {
	store := s.inboundFragmentStore()
	if store == nil {
		return nil
	}
	if err := store.DeleteInboundFragments(s.inboundFragmentScope(key)); err != nil {
		return fmt.Errorf("delete persisted SMS fragment session %q: %w", key, err)
	}
	return nil
}

func storedFragmentFromRuntime(fragment *smsFragment) smsInboundFragmentRecord {
	if fragment == nil {
		return smsInboundFragmentRecord{}
	}
	return smsInboundFragmentRecord{
		Reference: fragment.Ref, ReferenceBits: fragment.RefBits,
		Total: fragment.Total, Sequence: fragment.Seq, Content: fragment.Content,
		ArrivedAt: fragment.Time, RPMR: int(fragment.RpMr), CallID: fragment.CallID,
		ToURI: fragment.ToURI, ServiceCenter: fragment.ServiceCenter,
		AckSent: fragment.AckSent, AckSentAt: fragment.AckSentAt,
		DegradedAt: fragment.DegradedAt,
	}
}

func runtimeFragmentsFromStored(records []smsInboundFragmentRecord) []*smsFragment {
	fragments := make([]*smsFragment, 0, len(records))
	for _, record := range records {
		fragments = append(fragments, runtimeFragmentFromStored(record))
	}
	return fragments
}

func runtimeFragmentFromStored(record smsInboundFragmentRecord) *smsFragment {
	return &smsFragment{
		Ref: record.Reference, RefBits: record.ReferenceBits,
		Total: record.Total, Seq: record.Sequence, Content: record.Content,
		Time: record.ArrivedAt, RpMr: uint8(record.RPMR), CallID: record.CallID,
		ToURI: record.ToURI, ServiceCenter: record.ServiceCenter,
		AckSent: record.AckSent, AckSentAt: record.AckSentAt,
		DegradedAt: record.DegradedAt,
	}
}

func fragmentLateWindowExpired(fragments []*smsFragment, now time.Time) bool {
	degradedAt := fragmentDegradedAt(fragments)
	return !degradedAt.IsZero() && !now.Before(degradedAt.Add(inboundSMSLateReassemblyTTL))
}

func fragmentDegradedAt(fragments []*smsFragment) time.Time {
	var earliest time.Time
	for _, fragment := range fragments {
		if fragment == nil || fragment.DegradedAt.IsZero() {
			continue
		}
		if earliest.IsZero() || fragment.DegradedAt.Before(earliest) {
			earliest = fragment.DegradedAt
		}
	}
	return earliest
}

func latestFragmentTime(fragments []*smsFragment) time.Time {
	_, latest := fragmentBounds(fragments)
	return latest
}
