package pcsc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const usimAIDPrefix = "A0000000871002"

type Service struct {
	locksMu sync.Mutex
	locks   map[string]*readerGate
	backend Backend
}

func New() *Service { return NewWithBackend(newSystemBackend()) }

func NewWithBackend(backend Backend) *Service {
	return &Service{backend: backend, locks: make(map[string]*readerGate)}
}

func DeviceID(reader Reader) string {
	stable := strings.TrimSpace(reader.USBPath)
	if stable == "" {
		stable = strings.TrimSpace(reader.Name)
	}
	digest := sha256.Sum256([]byte(stable))
	return "pcsc-" + hex.EncodeToString(digest[:6])
}

func (service *Service) Readers(ctx context.Context) ([]Reader, error) {
	if service == nil || service.backend == nil {
		return nil, ErrUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return service.backend.Readers(ctx)
}

func selectorLockKey(selector Selector) string {
	if path := strings.TrimSpace(selector.USBPath); path != "" {
		return "path:" + path
	}
	return "name:" + strings.TrimSpace(selector.ReaderName)
}

func canonicalSelector(reader Reader) Selector {
	return Selector{USBPath: strings.TrimSpace(reader.USBPath), ReaderName: strings.TrimSpace(reader.Name)}
}

func (service *Service) resolveSelector(ctx context.Context, selector Selector) (Selector, error) {
	readers, err := service.backend.Readers(ctx)
	if err != nil {
		return Selector{}, err
	}
	reader, ok := MatchReader(readers, selector)
	if !ok {
		return Selector{}, ErrReaderNotFound
	}
	if !reader.CardPresent {
		return Selector{}, ErrNoCard
	}
	return canonicalSelector(reader), nil
}

type readerGate struct {
	token chan struct{}
}

func newReaderGate() *readerGate {
	gate := &readerGate{token: make(chan struct{}, 1)}
	gate.token <- struct{}{}
	return gate
}

func (gate *readerGate) acquire(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-gate.token:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (gate *readerGate) release() { gate.token <- struct{}{} }

func (service *Service) readerLock(selector Selector) *readerGate {
	key := selectorLockKey(selector)
	service.locksMu.Lock()
	defer service.locksMu.Unlock()
	if service.locks == nil {
		service.locks = make(map[string]*readerGate)
	}
	lock := service.locks[key]
	if lock == nil {
		lock = newReaderGate()
		service.locks[key] = lock
	}
	return lock
}

// Session owns the service lock for the whole card transaction. AKA and eSIM
// operations therefore cannot interleave APDUs on the same reader.
type Session struct {
	mu     sync.Mutex
	lock   *readerGate
	card   Card
	closed bool
}

func (service *Service) OpenSession(ctx context.Context, selector Selector) (*Session, error) {
	if service == nil || service.backend == nil {
		return nil, ErrUnavailable
	}
	if err := selector.Validate(); err != nil {
		return nil, err
	}
	resolved, err := service.resolveSelector(ctx, selector)
	if err != nil {
		return nil, err
	}
	lock := service.readerLock(resolved)
	if err := lock.acquire(ctx); err != nil {
		return nil, err
	}
	card, err := service.backend.Open(ctx, resolved)
	if err != nil {
		lock.release()
		return nil, err
	}
	return &Session{lock: lock, card: card}, nil
}

func (session *Session) Transmit(ctx context.Context, command []byte) ([]byte, uint16, error) {
	if session == nil {
		return nil, 0, errors.New("pcsc: card session is closed")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.card == nil || session.closed {
		return nil, 0, errors.New("pcsc: card session is closed")
	}
	return session.card.Transmit(ctx, command)
}

func (session *Session) Close() error { return session.close(false) }

func (session *Session) CloseWithReset() error { return session.close(true) }

func (session *Session) close(reset bool) error {
	if session == nil {
		return nil
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return nil
	}
	session.closed = true
	var err error
	if reset {
		if card, ok := session.card.(resettableCard); ok {
			err = card.CloseWithReset()
		} else {
			err = session.card.Close()
		}
	} else {
		err = session.card.Close()
	}
	session.lock.release()
	return err
}

func (service *Service) Snapshot(ctx context.Context, selector Selector, pin string) (Snapshot, error) {
	readers, err := service.Readers(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	reader, ok := MatchReader(readers, selector)
	if !ok {
		return Snapshot{}, ErrReaderNotFound
	}
	result := Snapshot{Reader: reader}
	if !reader.CardPresent {
		return result, ErrNoCard
	}
	result.Identity, err = service.ReadIdentity(ctx, selector, pin)
	return result, err
}

func (service *Service) ReadIdentity(ctx context.Context, selector Selector, pin string) (Identity, error) {
	session, err := service.OpenSession(ctx, selector)
	if err != nil {
		return Identity{}, err
	}
	defer session.Close()
	return readIdentity(ctx, session.card, pin)
}

func MatchReader(readers []Reader, selector Selector) (Reader, bool) {
	path := strings.TrimSpace(selector.USBPath)
	name := strings.TrimSpace(selector.ReaderName)
	for _, reader := range readers {
		if path != "" && reader.USBPath == path {
			return reader, true
		}
	}
	for _, reader := range readers {
		if name != "" && reader.Name == name {
			return reader, true
		}
	}
	return Reader{}, false
}

func requireService(service *Service) error {
	if service == nil || service.backend == nil {
		return ErrUnavailable
	}
	return nil
}

func changedCard(expected, actual string) bool {
	return strings.TrimSpace(expected) != "" && !strings.EqualFold(strings.TrimSpace(expected), actual)
}

func (service *Service) CheckReady(ctx context.Context, selector Selector, expectedICCID, pin string) (string, error) {
	if err := requireService(service); err != nil {
		return "", err
	}
	session, err := service.OpenSession(ctx, selector)
	if err != nil {
		return "", err
	}
	defer session.Close()
	iccid, err := readICCID(ctx, session.card)
	if err != nil {
		return "", err
	}
	if changedCard(expectedICCID, iccid) {
		return "", ErrCardChanged
	}
	aid, err := selectUSIM(ctx, session.card)
	if err != nil {
		return "", err
	}
	if err := verifyPIN(ctx, session.card, pin); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(aid)), nil
}

func statusError(operation string, sw uint16) error {
	if sw == 0x9862 {
		return ErrAKARejected
	}
	return fmt.Errorf("pcsc: %s failed with status %04X", operation, sw)
}

func (service *Service) Authenticate(ctx context.Context, selector Selector, expectedICCID, pin string, challenge AKAChallenge) (AKAResult, error) {
	session, err := service.OpenSession(ctx, selector)
	if err != nil {
		return AKAResult{}, err
	}
	defer session.Close()
	iccid, err := readICCID(ctx, session.card)
	if err != nil {
		return AKAResult{}, err
	}
	if changedCard(expectedICCID, iccid) {
		return AKAResult{}, ErrCardChanged
	}
	if _, err := selectUSIM(ctx, session.card); err != nil {
		return AKAResult{}, err
	}
	if err := verifyPIN(ctx, session.card, pin); err != nil {
		return AKAResult{}, err
	}
	apdu := []byte{0x00, 0x88, 0x00, 0x81, 0x22, 0x10}
	apdu = append(apdu, challenge.RAND[:]...)
	apdu = append(apdu, 0x10)
	apdu = append(apdu, challenge.AUTN[:]...)
	apdu = append(apdu, 0x00)
	data, sw, err := session.card.Transmit(ctx, apdu)
	if err != nil {
		return AKAResult{}, fmt.Errorf("pcsc: USIM authentication transport failed: %w", err)
	}
	if sw != 0x9000 {
		return AKAResult{}, statusError("USIM authentication", sw)
	}
	return parseAKAResponse(data)
}
