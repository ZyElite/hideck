package phone

import (
	"path/filepath"
	"strings"

	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

func (s *Service) finalizeRecording(event voicehost.CallEvent) {
	s.mu.RLock()
	call := s.calls[event.CallID]
	s.mu.RUnlock()
	if call == nil {
		return
	}
	recordingName, recordingErr := s.publishMP3(event.AudioPath)
	s.mu.Lock()
	call.record.PCAPName = baseName(event.PCAPPath)
	call.record.Codec = event.AudioCodec
	call.record.RecordingName = recordingName
	call.record.RecordingError = joinErrors(event.RecordingError, recordingErr)
	call.view.Codec, call.view.RecordingError = call.record.Codec, call.record.RecordingError
	record := call.record
	delete(s.calls, event.CallID)
	s.mu.Unlock()
	s.persist(record)
	kind := "recording_ready"
	if record.RecordingError != "" {
		kind = "recording_failed"
	}
	s.publish(kind, call)
}

func (s *Service) publishMP3(audioPath string) (string, string) {
	if strings.TrimSpace(audioPath) == "" {
		return "", ""
	}
	if s.transcoder == nil {
		return "", "phone: MP3 transcoder is unavailable"
	}
	output, err := s.transcoder.ToMP3(s.ctx, audioPath)
	if err != nil {
		return "", err.Error()
	}
	return baseName(output), ""
}

func baseName(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return filepath.Base(path)
}

func joinErrors(values ...string) string {
	nonEmpty := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			nonEmpty = append(nonEmpty, value)
		}
	}
	return strings.Join(nonEmpty, "; ")
}
