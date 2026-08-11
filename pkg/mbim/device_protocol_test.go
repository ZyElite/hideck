package mbim

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func functionErrorMsg(tx uint32, code ProtocolErrorCode) []byte {
	return encodeProtocolError(MessageTypeFunctionError, tx, code)
}

func protocolErrorCode(t *testing.T, err error) ProtocolErrorCode {
	t.Helper()
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("error = %v, want *ProtocolError", err)
	}
	return protocolErr.Code
}

func writtenHeaders(t *testing.T, transport *fakeTransport) []Header {
	t.Helper()
	transport.mu.Lock()
	defer transport.mu.Unlock()
	headers := make([]Header, 0, len(transport.written))
	for _, message := range transport.written {
		header, err := decodeHeader(message)
		if err != nil {
			t.Fatalf("decode written message: %v", err)
		}
		headers = append(headers, header)
	}
	return headers
}

func requireHostError(t *testing.T, transport *fakeTransport, code ProtocolErrorCode) {
	t.Helper()
	transport.mu.Lock()
	defer transport.mu.Unlock()
	for _, message := range transport.written {
		header, err := decodeHeader(message)
		if err == nil && header.Type == MessageTypeHostError && le.Uint32(message[headerLen:]) == uint32(code) {
			return
		}
	}
	t.Fatalf("HOST_ERROR code=%d was not written", code)
}

func TestDeviceOpenReturnsFunctionErrorImmediately(t *testing.T) {
	transport := newFakeTransport()
	transport.reply = func(message []byte) ([]byte, bool) {
		header, _ := decodeHeader(message)
		if header.Type == MessageTypeOpen {
			return functionErrorMsg(header.TransactionID, ProtocolErrorMaxTransfer), true
		}
		return nil, false
	}
	device := newDevice(transport)
	defer device.Close()

	started := time.Now()
	err := device.Open(context.Background(), 4096)
	if code := protocolErrorCode(t, err); code != ProtocolErrorMaxTransfer {
		t.Fatalf("protocol error code = %d, want %d", code, ProtocolErrorMaxTransfer)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("FUNCTION_ERROR surfaced after %s, want immediate failure", elapsed)
	}
}

func TestDeviceCommandReturnsFunctionError(t *testing.T) {
	transport := newFakeTransport()
	transport.reply = func(message []byte) ([]byte, bool) {
		header, _ := decodeHeader(message)
		switch header.Type {
		case MessageTypeOpen:
			return openDoneMsg(header.TransactionID), true
		case MessageTypeCommand:
			return functionErrorMsg(header.TransactionID, ProtocolErrorNotOpened), true
		}
		return nil, false
	}
	device := newDevice(transport)
	defer device.Close()
	if err := device.Open(context.Background(), 4096); err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, err := device.Command(context.Background(), UUIDBasicConnect, CIDBasicConnectDeviceCaps, CommandTypeQuery, nil)
	if code := protocolErrorCode(t, err); code != ProtocolErrorNotOpened {
		t.Fatalf("protocol error code = %d, want %d", code, ProtocolErrorNotOpened)
	}
}

func TestDeviceCloseWaitsForCloseDone(t *testing.T) {
	transport := newFakeTransport()
	transport.reply = func(message []byte) ([]byte, bool) {
		header, _ := decodeHeader(message)
		if header.Type == MessageTypeOpen {
			return openDoneMsg(header.TransactionID), true
		}
		return nil, false
	}
	device := newDevice(transport)
	if err := device.Open(context.Background(), 4096); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := device.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	headers := writtenHeaders(t, transport)
	if got := headers[len(headers)-1].Type; got != MessageTypeClose {
		t.Fatalf("last message type = 0x%x, want CLOSE", got)
	}
}

func TestDeviceCloseContextReportsMissingCloseDone(t *testing.T) {
	transport := newFakeTransport()
	transport.disableDefaultClose = true
	transport.reply = func(message []byte) ([]byte, bool) {
		header, _ := decodeHeader(message)
		if header.Type == MessageTypeOpen {
			return openDoneMsg(header.TransactionID), true
		}
		return nil, false
	}
	device := newDevice(transport)
	if err := device.Open(context.Background(), 4096); err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := device.CloseContext(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CloseContext error = %v, want deadline exceeded", err)
	}
	device.mu.Lock()
	closed := device.closed
	device.mu.Unlock()
	if !closed {
		t.Fatal("transport state must be closed after CLOSE_DONE timeout")
	}
}

func TestDeviceTransactionIDWrapSkipsZero(t *testing.T) {
	transport := newFakeTransport()
	var openTransaction uint32
	transport.reply = func(message []byte) ([]byte, bool) {
		header, _ := decodeHeader(message)
		if header.Type == MessageTypeOpen {
			openTransaction = header.TransactionID
			return openDoneMsg(header.TransactionID), true
		}
		return nil, false
	}
	device := newDevice(transport)
	device.nextTx = math.MaxUint32
	defer device.Close()
	if err := device.Open(context.Background(), 4096); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if openTransaction != 1 {
		t.Fatalf("wrapped OPEN transaction ID = %d, want 1", openTransaction)
	}
}

func TestDeviceOpenRetriesWithNewTransactionID(t *testing.T) {
	transport := newFakeTransport()
	var transactions []uint32
	transport.reply = func(message []byte) ([]byte, bool) {
		header, _ := decodeHeader(message)
		if header.Type != MessageTypeOpen {
			return nil, false
		}
		transactions = append(transactions, header.TransactionID)
		if len(transactions) == 2 {
			return openDoneMsg(header.TransactionID), true
		}
		return nil, false
	}
	device := newDevice(transport)
	device.openAttemptTimeout = 10 * time.Millisecond
	defer device.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := device.Open(ctx, 4096); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(transactions) != 2 || transactions[0] == 0 || transactions[0] == transactions[1] {
		t.Fatalf("OPEN transaction IDs = %v, want two distinct nonzero IDs", transactions)
	}
}

func TestDeviceCommandCancellationSendsHostError(t *testing.T) {
	transport := newFakeTransport()
	transport.reply = func(message []byte) ([]byte, bool) {
		header, _ := decodeHeader(message)
		if header.Type == MessageTypeOpen {
			return openDoneMsg(header.TransactionID), true
		}
		return nil, false
	}
	device := newDevice(transport)
	defer device.Close()
	if err := device.Open(context.Background(), 4096); err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := device.Command(ctx, UUIDBasicConnect, CIDBasicConnectDeviceCaps, CommandTypeQuery, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Command error = %v, want deadline exceeded", err)
	}
	requireHostError(t, transport, ProtocolErrorCancel)
}

func TestDeviceRejectsOutOfOrderFragment(t *testing.T) {
	transport := newFakeTransport()
	transport.reply = func(message []byte) ([]byte, bool) {
		header, _ := decodeHeader(message)
		switch header.Type {
		case MessageTypeOpen:
			return openDoneMsg(header.TransactionID), true
		case MessageTypeCommand:
			return makeCommandDoneFragment(header.TransactionID, 2, 1, 0, []byte{1}, false), true
		}
		return nil, false
	}
	device := newDevice(transport)
	defer device.Close()
	if err := device.Open(context.Background(), 4096); err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err := device.Command(context.Background(), UUIDBasicConnect, CIDBasicConnectDeviceCaps, CommandTypeQuery, nil)
	if code := protocolErrorCode(t, err); code != ProtocolErrorFragmentSequence {
		t.Fatalf("protocol error code = %d, want %d", code, ProtocolErrorFragmentSequence)
	}
	requireHostError(t, transport, ProtocolErrorFragmentSequence)
}

func TestDeviceFragmentTimeoutSendsHostError(t *testing.T) {
	transport := newFakeTransport()
	transport.reply = func(message []byte) ([]byte, bool) {
		header, _ := decodeHeader(message)
		switch header.Type {
		case MessageTypeOpen:
			return openDoneMsg(header.TransactionID), true
		case MessageTypeCommand:
			fragment := makeCommandDoneFragment(header.TransactionID, 2, 0, 0, []byte{1, 2}, true)
			fragment = fragment[:fixedDoneOffset+1]
			le.PutUint32(fragment[4:], uint32(len(fragment)))
			return fragment, true
		}
		return nil, false
	}
	device := newDevice(transport)
	device.fragmentTimeout = 10 * time.Millisecond
	defer device.Close()
	if err := device.Open(context.Background(), 4096); err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err := device.Command(context.Background(), UUIDBasicConnect, CIDBasicConnectDeviceCaps, CommandTypeQuery, nil)
	if code := protocolErrorCode(t, err); code != ProtocolErrorTimeoutFragment {
		t.Fatalf("protocol error code = %d, want %d", code, ProtocolErrorTimeoutFragment)
	}
	requireHostError(t, transport, ProtocolErrorTimeoutFragment)
}

func TestDeviceRejectsDeclaredMessageLengthMismatch(t *testing.T) {
	transport := newFakeTransport()
	transport.reply = func(message []byte) ([]byte, bool) {
		header, _ := decodeHeader(message)
		switch header.Type {
		case MessageTypeOpen:
			return openDoneMsg(header.TransactionID), true
		case MessageTypeCommand:
			response := makeCommandDoneFragmentFor(header.TransactionID, UUIDBasicConnect, CIDBasicConnectDeviceCaps, nil)
			le.PutUint32(response[4:], uint32(len(response)+1))
			return response, true
		}
		return nil, false
	}
	device := newDevice(transport)
	defer device.Close()
	if err := device.Open(context.Background(), 4096); err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err := device.Command(context.Background(), UUIDBasicConnect, CIDBasicConnectDeviceCaps, CommandTypeQuery, nil)
	if code := protocolErrorCode(t, err); code != ProtocolErrorLengthMismatch {
		t.Fatalf("protocol error code = %d, want %d", code, ProtocolErrorLengthMismatch)
	}
	requireHostError(t, transport, ProtocolErrorLengthMismatch)
}
