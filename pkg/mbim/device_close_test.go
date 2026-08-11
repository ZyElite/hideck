package mbim

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestDeviceCloseCancelsPendingCommandBeforeCloseMessage(t *testing.T) {
	transport := newFakeTransport()
	commandWritten := make(chan struct{})
	var commandOnce sync.Once
	transport.reply = func(message []byte) ([]byte, bool) {
		header, _ := decodeHeader(message)
		switch header.Type {
		case MessageTypeOpen:
			return openDoneMsg(header.TransactionID), true
		case MessageTypeCommand:
			commandOnce.Do(func() { close(commandWritten) })
		}
		return nil, false
	}
	device := newDevice(transport)
	if err := device.Open(context.Background(), 4096); err != nil {
		t.Fatalf("Open: %v", err)
	}

	commandResult := make(chan error, 1)
	go func() {
		_, err := device.Command(context.Background(), UUIDBasicConnect, CIDBasicConnectDeviceCaps, CommandTypeQuery, nil)
		commandResult <- err
	}()
	select {
	case <-commandWritten:
	case <-time.After(time.Second):
		t.Fatal("pending command was not written")
	}
	if err := device.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-commandResult:
		if err == nil {
			t.Fatal("pending command returned success during close")
		}
	case <-time.After(time.Second):
		t.Fatal("pending command was not canceled by close")
	}

	headers := writtenHeaders(t, transport)
	commandIndex := messageTypeIndex(headers, MessageTypeCommand)
	hostErrorIndex := messageTypeIndex(headers, MessageTypeHostError)
	closeIndex := messageTypeIndex(headers, MessageTypeClose)
	if commandIndex < 0 || hostErrorIndex <= commandIndex || closeIndex <= hostErrorIndex {
		t.Fatalf("message order = %v, want COMMAND then HOST_ERROR then CLOSE", headers)
	}
	requireHostError(t, transport, ProtocolErrorCancel)
}

func messageTypeIndex(headers []Header, messageType MessageType) int {
	for index, header := range headers {
		if header.Type == messageType {
			return index
		}
	}
	return -1
}
