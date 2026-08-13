package phone

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

func (s *Service) startMixedRecording(call *activeCall, media *MediaSession) {
	if call == nil || media == nil {
		return
	}
	call.mixedRecordOnce.Do(func() {
		path, pathErr := s.mixedRecordingPath(call)
		var recorder *mixedRecorder
		var recorderErr error
		if pathErr == nil {
			recorder, recorderErr = newMixedRecorder(path)
		}
		recordingErr := errors.Join(pathErr, recorderErr)
		s.mu.Lock()
		call.mixedAttempted = true
		terminal := call.terminal
		if !terminal {
			call.mixedRecorder, call.mixedAudioPath = recorder, path
		}
		s.mu.Unlock()
		if terminal && recorder != nil {
			recordingErr = errors.Join(
				recordingErr, errors.New("phone: call ended before mixed recording started"), recorder.Close(),
			)
		}
		s.mu.Lock()
		if recordingErr != nil {
			call.record.RecordingError = joinErrors(call.record.RecordingError, recordingErr.Error())
			call.view.RecordingError = call.record.RecordingError
		}
		record := call.record
		s.mu.Unlock()
		if recordingErr != nil {
			s.persist(record)
			s.publish("recording_failed", call)
		}
	})
	s.attachMixedRecorder(call, media)
}

func (s *Service) mixedRecordingPath(call *activeCall) (string, error) {
	if strings.TrimSpace(s.recordingDir) == "" {
		return "", errors.New("phone: mixed recording directory is not configured")
	}
	if strings.TrimSpace(call.recordingBase) == "" {
		return "", errors.New("phone: mixed recording base path is empty")
	}
	return call.recordingBase + "_mixed.wav", nil
}

func (s *Service) attachMixedRecorder(call *activeCall, media *MediaSession) {
	if call == nil || media == nil {
		return
	}
	s.mu.RLock()
	recorder := call.mixedRecorder
	s.mu.RUnlock()
	if recorder != nil {
		media.SetRecorder(recorder)
	}
}

func (s *Service) stopMixedRecording(call *activeCall) {
	if call == nil {
		return
	}
	s.mu.RLock()
	recorder := call.mixedRecorder
	s.mu.RUnlock()
	if recorder == nil {
		return
	}
	err := recorder.Close()
	s.mu.Lock()
	call.mixedRecorder = nil
	if err != nil {
		call.record.RecordingError = joinErrors(call.record.RecordingError, err.Error())
		call.view.RecordingError = call.record.RecordingError
	}
	s.mu.Unlock()
}

func (s *Service) stopAllMixedRecordings(calls []*activeCall) {
	for _, call := range calls {
		s.stopMixedRecording(call)
		s.mu.RLock()
		record := call.record
		s.mu.RUnlock()
		s.persist(record)
	}
}

func (s *Service) finalizeRecording(event voicehost.CallEvent) {
	s.mu.RLock()
	call := s.calls[event.CallID]
	if call == nil {
		s.mu.RUnlock()
		return
	}
	mixedPath, mixedAttempted := call.mixedAudioPath, call.mixedAttempted
	s.mu.RUnlock()
	audioPath := ""
	if mixedAttempted {
		audioPath = mixedPath
	}
	recordingName, recordingErr := s.publishMP3(audioPath)
	s.mu.Lock()
	call.record.PCAPName = baseName(event.PCAPPath)
	if strings.TrimSpace(event.AudioCodec) != "" {
		call.record.Codec = event.AudioCodec
	}
	call.record.RecordingName = recordingName
	call.record.RecordingError = joinErrors(
		call.record.RecordingError, event.RecordingError, recordingErr,
	)
	call.view.Codec, call.view.RecordingError = call.record.Codec, call.record.RecordingError
	record := call.record
	delete(s.calls, event.CallID)
	s.mu.Unlock()
	s.persist(record)
	kind := "recording_ready"
	if record.RecordingError != "" {
		kind = "recording_failed"
	} else if record.RecordingName == "" {
		kind = "recording_unavailable"
	}
	s.publish(kind, call)
	call.finalizedOnce.Do(func() {
		if call.finalizedDone != nil {
			close(call.finalizedDone)
		}
	})
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
	seen := make(map[string]struct{}, len(values))
	nonEmpty := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		nonEmpty = append(nonEmpty, value)
	}
	return strings.Join(nonEmpty, "; ")
}
