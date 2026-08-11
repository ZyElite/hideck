package mbim

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDeviceRejectsMismatchedResponseType(t *testing.T) {
	transport := newFakeTransport()
	transport.reply = func(message []byte) ([]byte, bool) {
		header, _ := decodeHeader(message)
		switch header.Type {
		case MessageTypeOpen:
			return openDoneMsg(header.TransactionID), true
		case MessageTypeCommand:
			return openDoneMsg(header.TransactionID), true
		}
		return nil, false
	}
	device := newDevice(transport)
	defer device.Close()
	if err := device.Open(context.Background(), 4096); err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, err := device.Command(context.Background(), UUIDBasicConnect, CIDBasicConnectDeviceCaps, CommandTypeQuery, nil)
	if code := protocolErrorCode(t, err); code != ProtocolErrorUnknown {
		t.Fatalf("protocol error code = %d, want %d", code, ProtocolErrorUnknown)
	}
	requireHostError(t, transport, ProtocolErrorUnknown)
}

func TestDeviceRejectsMismatchedCommandServiceAndCID(t *testing.T) {
	transport := newFakeTransport()
	transport.reply = func(message []byte) ([]byte, bool) {
		header, _ := decodeHeader(message)
		switch header.Type {
		case MessageTypeOpen:
			return openDoneMsg(header.TransactionID), true
		case MessageTypeCommand:
			return makeCommandDoneFragmentFor(header.TransactionID, UUIDSMS, CIDSMSSend, nil), true
		}
		return nil, false
	}
	device := newDevice(transport)
	defer device.Close()
	if err := device.Open(context.Background(), 4096); err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, err := device.Command(context.Background(), UUIDBasicConnect, CIDBasicConnectDeviceCaps, CommandTypeQuery, nil)
	if code := protocolErrorCode(t, err); code != ProtocolErrorUnknown {
		t.Fatalf("protocol error code = %d, want %d", code, ProtocolErrorUnknown)
	}
	requireHostError(t, transport, ProtocolErrorUnknown)
}

func TestDeviceProtocolErrorQueuePreservesBurst(t *testing.T) {
	device := newDevice(newFakeTransport())
	defer device.Close()
	const errorCount = 32
	for i := 1; i <= errorCount; i++ {
		device.reportProtocolError(&ProtocolError{Code: ProtocolErrorUnknown, TransactionID: uint32(i)})
	}

	for want := 1; want <= errorCount; want++ {
		select {
		case err := <-device.ProtocolErrors():
			var protocolErr *ProtocolError
			if !errors.As(err, &protocolErr) || protocolErr.TransactionID != uint32(want) {
				t.Fatalf("queued error %d = %v", want, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for queued protocol error %d", want)
		}
	}
}
