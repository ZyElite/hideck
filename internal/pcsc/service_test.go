package pcsc

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type scriptedReply struct {
	data []byte
	sw   uint16
	err  error
}

type scriptedCard struct {
	replies []scriptedReply
	calls   [][]byte
	closed  bool
}

func (card *scriptedCard) Transmit(_ context.Context, command []byte) ([]byte, uint16, error) {
	card.calls = append(card.calls, append([]byte(nil), command...))
	if len(card.replies) == 0 {
		return nil, 0, errors.New("unexpected APDU")
	}
	reply := card.replies[0]
	card.replies = card.replies[1:]
	return append([]byte(nil), reply.data...), reply.sw, reply.err
}

func (card *scriptedCard) Close() error { card.closed = true; return nil }

type scriptedBackend struct {
	card *scriptedCard
}

func (backend *scriptedBackend) Readers(context.Context) ([]Reader, error) {
	return []Reader{{Name: "Reader A", USBPath: "1-2", CardPresent: true}}, nil
}

func (backend *scriptedBackend) Open(context.Context, Selector) (Card, error) {
	return backend.card, nil
}

func TestDecodeIdentifiers(t *testing.T) {
	if got := decodeSwappedBCD([]byte{0x98, 0x10, 0x32, 0x54, 0xF6}, false); got != "890123456" {
		t.Fatalf("ICCID BCD = %q", got)
	}
	imsi, err := decodeIMSI([]byte{0x08, 0x19, 0x32, 0x54, 0x76, 0x98, 0x10, 0x32, 0x54})
	if err != nil {
		t.Fatal(err)
	}
	if imsi != "123456789012345" {
		t.Fatalf("IMSI = %q", imsi)
	}
}

func TestVerifyPINRequiresConfiguredPIN(t *testing.T) {
	card := &scriptedCard{replies: []scriptedReply{{sw: 0x63C3}}}
	err := verifyPIN(context.Background(), card, "")
	if !errors.Is(err, ErrPINRequired) {
		t.Fatalf("error = %v", err)
	}
	var pinErr *PINError
	if !errors.As(err, &pinErr) || pinErr.Tries != 3 {
		t.Fatalf("PIN error = %#v", pinErr)
	}
	if len(card.calls) != 1 {
		t.Fatalf("APDU calls = %d, expected status check only", len(card.calls))
	}
}

func TestVerifyPINRefusesLowAttemptCount(t *testing.T) {
	card := &scriptedCard{replies: []scriptedReply{{sw: 0x63C2}}}
	err := verifyPIN(context.Background(), card, "1234")
	if !errors.Is(err, ErrPINTriesLow) {
		t.Fatalf("error = %v", err)
	}
	if len(card.calls) != 1 {
		t.Fatalf("APDU calls = %d, PIN must not be submitted", len(card.calls))
	}
}

func TestVerifyPINDoesNotMaskUnknownStatus(t *testing.T) {
	card := &scriptedCard{replies: []scriptedReply{{sw: 0x6D00}}}
	err := verifyPIN(context.Background(), card, "")
	if err == nil || errors.Is(err, ErrPINRequired) {
		t.Fatalf("error = %v, want explicit unknown status", err)
	}
}

func TestParseAKAResponse(t *testing.T) {
	data := []byte{0xDB, 0x08, 1, 2, 3, 4, 5, 6, 7, 8, 0x10}
	data = append(data, bytes.Repeat([]byte{0xAA}, 16)...)
	data = append(data, 0x10)
	data = append(data, bytes.Repeat([]byte{0xBB}, 16)...)
	result, err := parseAKAResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RES) != 8 || len(result.CK) != 16 || len(result.IK) != 16 || result.SynchronizationFailure {
		t.Fatalf("unexpected AKA result: %#v", result)
	}

	syncResult, err := parseAKAResponse(append([]byte{0xDC, 0x0E}, bytes.Repeat([]byte{0xCC}, 14)...))
	if err != nil {
		t.Fatal(err)
	}
	if !syncResult.SynchronizationFailure || len(syncResult.AUTS) != 14 {
		t.Fatalf("unexpected sync result: %#v", syncResult)
	}
}

func TestDeviceIDUsesStableUSBPath(t *testing.T) {
	a := DeviceID(Reader{Name: "reader 00 00", USBPath: "1-3"})
	b := DeviceID(Reader{Name: "renamed reader", USBPath: "1-3"})
	if a != b || a == "" {
		t.Fatalf("device IDs = %q, %q", a, b)
	}
}

func TestLogicalChannelCLA(t *testing.T) {
	for _, test := range []struct {
		class, channel, want byte
	}{{0x80, 1, 0x81}, {0x80, 4, 0xC0}, {0x00, 19, 0x4F}} {
		if got := logicalChannelCLA(test.class, test.channel); got != test.want {
			t.Fatalf("logicalChannelCLA(%02X, %d) = %02X, want %02X", test.class, test.channel, got, test.want)
		}
	}
}

func TestLogicalChannelRewritesCLAAndCleansUp(t *testing.T) {
	card := &scriptedCard{replies: []scriptedReply{
		{data: []byte{4}, sw: 0x9000},
		{sw: 0x9000},
		{data: []byte{0xAA}, sw: 0x9000},
		{sw: 0x9000},
	}}
	service := NewWithBackend(&scriptedBackend{card: card})
	channel := NewChannel(service, Selector{USBPath: "1-2"}, false)
	if err := channel.Connect(); err != nil {
		t.Fatal(err)
	}
	logical, err := channel.OpenLogicalChannel([]byte{0xA0, 0x00})
	if err != nil || logical != 4 {
		t.Fatalf("OpenLogicalChannel() = %d, %v", logical, err)
	}
	response, err := channel.Transmit([]byte{0x80, 0xE2, 0x91, 0x00})
	if err != nil || !bytes.Equal(response, []byte{0xAA, 0x90, 0x00}) {
		t.Fatalf("Transmit() = % X, %v", response, err)
	}
	if got := card.calls[2][0]; got != 0xC0 {
		t.Fatalf("transmitted CLA = %02X, want C0", got)
	}
	if err := channel.CloseLogicalChannel(logical); err != nil {
		t.Fatal(err)
	}
	if err := channel.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if !card.closed {
		t.Fatal("card session was not closed")
	}
}

func TestLogicalChannelOpenFailureReleasesSession(t *testing.T) {
	card := &scriptedCard{replies: []scriptedReply{{sw: 0x6A81}}}
	service := NewWithBackend(&scriptedBackend{card: card})
	channel := NewChannel(service, Selector{USBPath: "1-2"}, false)
	if err := channel.Connect(); err != nil {
		t.Fatal(err)
	}
	if _, err := channel.OpenLogicalChannel([]byte{0xA0}); err == nil {
		t.Fatal("OpenLogicalChannel() error = nil")
	}
	if !card.closed {
		t.Fatal("failed logical channel left the card transaction open")
	}
}

func TestLogicalChannelReportsMissingApplication(t *testing.T) {
	card := &scriptedCard{replies: []scriptedReply{
		{data: []byte{1}, sw: 0x9000},
		{sw: 0x6A82},
		{sw: 0x9000},
	}}
	service := NewWithBackend(&scriptedBackend{card: card})
	channel := NewChannel(service, Selector{USBPath: "1-2"}, false)
	if err := channel.Connect(); err != nil {
		t.Fatal(err)
	}
	_, err := channel.OpenLogicalChannel([]byte{0xA0})
	if !errors.Is(err, ErrApplicationNotFound) {
		t.Fatalf("OpenLogicalChannel() error = %v, want ErrApplicationNotFound", err)
	}
	if !card.closed {
		t.Fatal("missing application left the card transaction open")
	}
}

func TestDecodeMultiString(t *testing.T) {
	want := []string{"Reader A", "Reader B"}
	got := decodeMultiString([]byte("Reader A\x00Reader B\x00\x00"))
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("decodeMultiString = %#v", got)
	}
}

type countingBackend struct {
	mu        sync.Mutex
	opens     int
	cardMaker func() Card
}

func (*countingBackend) Readers(context.Context) ([]Reader, error) {
	return []Reader{
		{Name: "Reader A", USBPath: "1-2", CardPresent: true},
		{Name: "Reader B", USBPath: "1-3", CardPresent: true},
	}, nil
}

func (backend *countingBackend) Open(context.Context, Selector) (Card, error) {
	backend.mu.Lock()
	backend.opens++
	backend.mu.Unlock()
	return backend.cardMaker(), nil
}

func TestOpenSessionWaitHonorsContext(t *testing.T) {
	backend := &countingBackend{cardMaker: func() Card { return &scriptedCard{} }}
	service := NewWithBackend(backend)
	selector := Selector{USBPath: "1-2"}
	first, err := service.OpenSession(context.Background(), selector)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := service.OpenSession(ctx, selector); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second OpenSession() error = %v, want deadline exceeded", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	opens := backend.opens
	backend.mu.Unlock()
	if opens != 1 {
		t.Fatalf("backend opens = %d, timed-out waiter must not open a card", opens)
	}
}

func TestOpenSessionLocksReadersIndependently(t *testing.T) {
	backend := &countingBackend{cardMaker: func() Card { return &scriptedCard{} }}
	service := NewWithBackend(backend)
	first, err := service.OpenSession(context.Background(), Selector{USBPath: "1-2"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.OpenSession(context.Background(), Selector{USBPath: "1-3"})
	if err != nil {
		t.Fatalf("independent reader was blocked: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenSessionSerializesReaderAliases(t *testing.T) {
	backend := &countingBackend{cardMaker: func() Card { return &scriptedCard{} }}
	service := NewWithBackend(backend)
	first, err := service.OpenSession(context.Background(), Selector{USBPath: "1-2"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := service.OpenSession(ctx, Selector{ReaderName: "Reader A"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("alias OpenSession() error = %v, want deadline exceeded", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
}
