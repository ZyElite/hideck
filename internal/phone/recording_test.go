package phone

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

func TestFinalizeRecordingPublishesMixedMP3AndPCAPMetadata(t *testing.T) {
	gateway, store := newFakeVoiceGateway(), newMemoryCallStore()
	service := newPhoneTestService(t, gateway, store, time.Second)
	transcoder := &recordingTranscoder{}
	service.transcoder = transcoder
	call := recordingTestCall(service, "recording-1")
	call.mixedAttempted = true
	call.mixedAudioPath = filepath.Join(t.TempDir(), "call_dev_mixed.wav")
	service.finalizeRecording(voicehost.CallEvent{
		Type: "CallFinalized", CallID: call.view.CallID,
		PCAPPath: filepath.Join(t.TempDir(), "call_dev.pcap"), AudioCodec: "PCMU",
	})
	record := store.record(call.view.CallID)
	if transcoder.input != call.mixedAudioPath || record.RecordingName != "call_dev_mixed.mp3" ||
		record.PCAPName != "call_dev.pcap" || record.Codec != "PCMU" || record.RecordingError != "" {
		t.Fatalf("transcoder input=%q record=%+v", transcoder.input, record)
	}
}

func TestFinalizeRecordingKeepsFailureSeparateFromCompletedCall(t *testing.T) {
	gateway, store := newFakeVoiceGateway(), newMemoryCallStore()
	service := newPhoneTestService(t, gateway, store, time.Second)
	service.transcoder = &recordingTranscoder{err: errors.New("encoder failed")}
	call := recordingTestCall(service, "recording-failed-1")
	call.mixedAttempted = true
	call.mixedAudioPath = filepath.Join(t.TempDir(), "call_failed_mixed.wav")
	call.record.RecordingError = "capture warning"
	service.finalizeRecording(voicehost.CallEvent{
		Type: "CallFinalized", CallID: call.view.CallID, RecordingError: "capture warning",
	})
	record := store.record(call.view.CallID)
	if record.Status != StatusCompleted || record.RecordingName != "" ||
		!strings.Contains(record.RecordingError, "capture warning") ||
		!strings.Contains(record.RecordingError, "encoder failed") {
		t.Fatalf("record = %+v", record)
	}
	backlog, _, cancel := service.Subscribe(0)
	defer cancel()
	if len(backlog) == 0 || backlog[len(backlog)-1].Type != "recording_failed" {
		t.Fatalf("events = %+v", backlog)
	}
}

type recordingTranscoder struct {
	input string
	err   error
}

func (transcoder *recordingTranscoder) ToMP3(_ context.Context, input string) (string, error) {
	transcoder.input = input
	if transcoder.err != nil {
		return "", transcoder.err
	}
	return strings.TrimSuffix(input, filepath.Ext(input)) + ".mp3", nil
}

func recordingTestCall(service *Service, callID string) *activeCall {
	now := time.Now()
	call := &activeCall{
		view: CallView{CallID: callID, DeviceID: "dev-1", Status: StatusCompleted, StartedAt: now},
		record: CallRecord{
			CallID: callID, DeviceID: "dev-1", Status: StatusCompleted, StartedAt: now,
		},
		terminal: true, terminalDone: make(chan struct{}), finalizedDone: make(chan struct{}),
	}
	service.mu.Lock()
	service.calls[callID] = call
	service.mu.Unlock()
	return call
}
