//go:build darwin || linux

package pcsc

import (
	"context"
	"errors"
	"sync"
	"unsafe"
)

const (
	pcscMaxResponseLength = 65546
	pcscMaxContinuations  = 8
)

type systemCard struct {
	api      *winscardAPI
	context  uintptr
	handle   uintptr
	protocol uint32
	closed   bool
	mu       sync.Mutex
}

func (card *systemCard) Transmit(ctx context.Context, command []byte) ([]byte, uint16, error) {
	card.mu.Lock()
	defer card.mu.Unlock()
	return card.transmit(ctx, append([]byte(nil), command...), 0)
}

func (card *systemCard) transmit(ctx context.Context, command []byte, depth int) ([]byte, uint16, error) {
	if card == nil || card.closed || card.api == nil {
		return nil, 0, errors.New("pcsc: card session is closed")
	}
	if depth > pcscMaxContinuations {
		return nil, 0, errors.New("pcsc: too many APDU continuations")
	}
	if err := contextError(ctx); err != nil {
		return nil, 0, err
	}
	response := make([]byte, pcscMaxResponseLength)
	responseLength := uint32(len(response))
	send := ioRequest{Protocol: card.protocol, Length: uint32(unsafe.Sizeof(ioRequest{}))}
	result := card.api.transmit(card.handle, &send, command, uint32(len(command)), nil, response, &responseLength)
	if err := checkPCSC("SCardTransmit", result); err != nil {
		return nil, 0, err
	}
	if responseLength < 2 || responseLength > uint32(len(response)) {
		return nil, 0, errors.New("pcsc: APDU response omitted its status word")
	}
	response = response[:responseLength]
	last := len(response) - 2
	data, sw := response[:last], uint16(response[last])<<8|uint16(response[last+1])
	if byte(sw>>8) == 0x6C && len(command) >= 5 {
		retry := append([]byte(nil), command...)
		retry[len(retry)-1] = byte(sw)
		return card.transmit(ctx, retry, depth+1)
	}
	if byte(sw>>8) == 0x61 || byte(sw>>8) == 0x9F {
		more, nextSW, err := card.transmit(ctx, []byte{command[0], 0xC0, 0x00, 0x00, byte(sw)}, depth+1)
		return append(append([]byte(nil), data...), more...), nextSW, err
	}
	return append([]byte(nil), data...), sw, contextError(ctx)
}

func (card *systemCard) Close() error { return card.close(pcscLeaveCard) }

func (card *systemCard) CloseWithReset() error { return card.close(pcscResetCard) }

func (card *systemCard) close(disposition uint32) error {
	card.mu.Lock()
	defer card.mu.Unlock()
	if card == nil || card.closed {
		return nil
	}
	card.closed = true
	var failures []error
	if err := checkPCSC("SCardEndTransaction", card.api.endTransaction(card.handle, disposition)); err != nil {
		failures = append(failures, err)
	}
	if err := checkPCSC("SCardDisconnect", card.api.disconnect(card.handle, disposition)); err != nil {
		failures = append(failures, err)
	}
	if err := checkPCSC("SCardReleaseContext", card.api.releaseContext(card.context)); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}
