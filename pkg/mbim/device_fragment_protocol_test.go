package mbim

import (
	"context"
	"testing"
	"time"
)

func TestDeviceDuplicateFirstFragmentPreservesOriginalAssembly(t *testing.T) {
	transport := newFakeTransport()
	commandTransaction := make(chan uint32, 1)
	var firstFragment []byte
	transport.reply = func(message []byte) ([]byte, bool) {
		header, _ := decodeHeader(message)
		switch header.Type {
		case MessageTypeOpen:
			return openDoneMsg(header.TransactionID), true
		case MessageTypeCommand:
			firstFragment = makeCommandDoneFragment(header.TransactionID, 2, 0, 0, []byte{1, 2}, true)
			firstFragment = firstFragment[:fixedDoneOffset+1]
			le.PutUint32(firstFragment[4:], uint32(len(firstFragment)))
			commandTransaction <- header.TransactionID
			return firstFragment, true
		}
		return nil, false
	}
	device := newDevice(transport)
	defer device.Close()
	if err := device.Open(context.Background(), 4096); err != nil {
		t.Fatalf("Open: %v", err)
	}

	resultCh := make(chan commandResult, 1)
	go func() {
		response, err := device.Command(context.Background(), UUIDBasicConnect, CIDBasicConnectDeviceCaps, CommandTypeQuery, nil)
		resultCh <- commandResult{resp: response, err: err}
	}()
	tx := <-commandTransaction
	waitForFragmentAssembly(t, device, tx)
	transport.toRead <- append([]byte(nil), firstFragment...)

	select {
	case err := <-device.ProtocolErrors():
		if code := protocolErrorCode(t, err); code != ProtocolErrorDuplicatedTID {
			t.Fatalf("protocol error code = %d, want %d", code, ProtocolErrorDuplicatedTID)
		}
	case <-time.After(time.Second):
		t.Fatal("duplicate transaction error was not reported")
	}
	requireHostError(t, transport, ProtocolErrorDuplicatedTID)
	transport.toRead <- makeCommandDoneFragment(tx, 2, 1, 0, []byte{2}, false)

	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf("Command: %v", result.err)
		}
		if string(result.resp.InfoBuffer) != string([]byte{1, 2}) {
			t.Fatalf("InfoBuffer = %v, want [1 2]", result.resp.InfoBuffer)
		}
	case <-time.After(time.Second):
		t.Fatal("original fragment assembly did not complete")
	}
}

func waitForFragmentAssembly(t *testing.T, device *Device, tx uint32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		device.mu.Lock()
		_, found := device.collector[tx]
		device.mu.Unlock()
		if found {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("fragment assembly tx=%d was not created", tx)
}
