package phone

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/iniwex5/vohive/pkg/logger"
	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

func (s *Service) handleIncoming(incoming voicehost.IncomingCall) {
	if strings.TrimSpace(incoming.CallID) == "" {
		return
	}
	startedAt := incoming.ReceivedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	call := s.newIncomingCall(incoming, startedAt)
	s.mu.Lock()
	if _, duplicate := s.calls[incoming.CallID]; duplicate {
		s.mu.Unlock()
		return
	}
	if _, busy := s.deviceCalls[incoming.DeviceID]; busy {
		s.mu.Unlock()
		err := s.gateway.RejectIncomingCall(voicehost.RejectRequest{
			DeviceID: incoming.DeviceID, CallID: incoming.CallID, StatusCode: 486,
		})
		s.recordBusyResult(voicehost.CallEvent{
			Type: "CallBusy", DeviceID: incoming.DeviceID, CallID: incoming.CallID,
			Caller: incoming.Caller, Time: startedAt,
		}, err)
		return
	}
	s.calls[incoming.CallID] = call
	s.deviceCalls[incoming.DeviceID] = incoming.CallID
	s.mu.Unlock()
	if err := s.gateway.StartCallCapture(incoming.DeviceID, incoming.CallID, call.recordingBase); err != nil {
		s.mu.Lock()
		call.record.RecordingError = err.Error()
		call.view.RecordingError = err.Error()
		s.mu.Unlock()
	}
	s.mu.RLock()
	record := call.record
	s.mu.RUnlock()
	s.persist(record)
	s.publish("incoming_call", call)
}

func (s *Service) newIncomingCall(incoming voicehost.IncomingCall, startedAt time.Time) *activeCall {
	peer := strings.TrimSpace(incoming.Caller)
	view := CallView{
		CallID: incoming.CallID, DeviceID: incoming.DeviceID, Direction: "inbound",
		Peer: peer, Status: StatusRinging, StartedAt: startedAt, ReadOnly: true,
	}
	record := CallRecord{
		CallID: incoming.CallID, DeviceID: incoming.DeviceID, ICCID: s.iccid(incoming.DeviceID),
		Direction: "inbound", Peer: peer, Status: StatusRinging, StartedAt: startedAt,
	}
	return &activeCall{
		view: view, record: record, incomingSDP: incoming.OfferSDP,
		recordingBase: s.captureBase(incoming.DeviceID, startedAt),
		terminalDone:  make(chan struct{}),
	}
}

func (s *Service) handleCallEvent(event voicehost.CallEvent) {
	if s.bufferPendingCallEvent(event) {
		return
	}
	s.dispatchCallEvent(event)
}

func (s *Service) bufferPendingCallEvent(event voicehost.CallEvent) bool {
	if event.CallID == "" || event.Type == "CallBusy" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls[event.CallID] != nil {
		return false
	}
	reservedCallID, reserved := s.deviceCalls[event.DeviceID]
	if !reserved || reservedCallID != "" {
		return false
	}
	s.pendingEvents[event.CallID] = append(s.pendingEvents[event.CallID], event)
	return true
}

func (s *Service) dispatchCallEvent(event voicehost.CallEvent) {
	switch event.Type {
	case "CallRinging":
		s.updateRinging(event.CallID)
	case "CallAnswered":
		s.updateAnswered(event)
	case "CallEnded", "CallFailed", "CallCanceled":
		s.finishCall(event)
	case "CallBusy":
		s.recordBusyResult(event, nil)
	case "CallFinalized":
		s.finalizeRecording(event)
	case "CallMediaUpdated":
		s.publishMediaUpdate(event)
	}
}

func (s *Service) updateRinging(callID string) {
	s.mu.Lock()
	call := s.calls[callID]
	if call == nil || call.terminal || call.view.Status == StatusConnected {
		s.mu.Unlock()
		return
	}
	call.view.Status, call.record.Status = StatusRinging, StatusRinging
	record := call.record
	s.mu.Unlock()
	s.persist(record)
	s.publish("call_ringing", call)
}

func (s *Service) updateAnswered(event voicehost.CallEvent) {
	s.mu.Lock()
	call := s.calls[event.CallID]
	if call == nil || call.terminal || call.view.AnsweredAt != nil {
		s.mu.Unlock()
		return
	}
	answeredAt := event.Time
	if answeredAt.IsZero() {
		answeredAt = time.Now()
	}
	call.view.Status, call.record.Status = StatusConnected, StatusConnected
	call.view.AnsweredAt, call.record.AnsweredAt = &answeredAt, &answeredAt
	mediaID, direction, deviceID := call.mediaID, call.view.Direction, call.view.DeviceID
	record := call.record
	s.mu.Unlock()
	if direction == "outbound" {
		s.attachOutboundMedia(event.CallID, deviceID, mediaID)
	}
	s.persist(record)
	s.publish("call_answered", call)
}

func (s *Service) attachOutboundMedia(callID, deviceID, mediaID string) {
	media := s.media.Get(mediaID)
	snapshot := s.gateway.ActiveCall(deviceID)
	if media == nil || snapshot == nil || snapshot.ClientSDP == "" {
		s.failMediaAttachment(callID, errors.New("phone: negotiated outbound media endpoint is unavailable"))
		return
	}
	endpoint, err := parseRTPEndpoint(snapshot.ClientSDP)
	if err == nil {
		s.mu.Lock()
		if call := s.calls[callID]; call != nil {
			call.view.Codec, call.record.Codec = endpoint.Codec, endpoint.Codec
		}
		s.mu.Unlock()
	}
	if err = media.Attach(snapshot.ClientSDP); err != nil {
		s.failMediaAttachment(callID, err)
	}
}

func (s *Service) failMediaAttachment(callID string, err error) {
	s.mu.Lock()
	call := s.calls[callID]
	if call == nil {
		s.mu.Unlock()
		return
	}
	call.view.EndReason, call.record.EndReason = err.Error(), err.Error()
	deviceID, resolvedCallID := call.view.DeviceID, call.view.CallID
	s.mu.Unlock()
	s.publish("media_error", call)
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	if hangupErr := s.gateway.HangupCall(ctx, deviceID, resolvedCallID); hangupErr != nil {
		logger.Error("媒体桥接失败后挂断失败", "device_id", deviceID, "call_id", resolvedCallID, "err", hangupErr)
	}
}

func (s *Service) finishCall(event voicehost.CallEvent) {
	s.mu.Lock()
	call := s.calls[event.CallID]
	if call == nil || call.terminal {
		s.mu.Unlock()
		return
	}
	endedAt := event.Time
	if endedAt.IsZero() {
		endedAt = time.Now()
	}
	status := terminalStatus(call, event)
	call.terminal, call.view.Status, call.record.Status = true, status, status
	call.view.EndedAt, call.record.EndedAt = &endedAt, &endedAt
	call.view.EndReason, call.record.EndReason = event.Reason, event.Reason
	call.record.DurationSeconds = callDurationSeconds(call.record, endedAt)
	if call.disconnectTimer != nil {
		call.disconnectTimer.Stop()
	}
	if s.deviceCalls[call.view.DeviceID] == call.view.CallID {
		delete(s.deviceCalls, call.view.DeviceID)
	}
	delete(s.mediaCalls, call.mediaID)
	s.terminalSeen[event.CallID] = struct{}{}
	mediaID, record := call.mediaID, call.record
	deviceID, peer, direction := call.view.DeviceID, call.view.Peer, call.view.Direction
	s.mu.Unlock()
	call.terminalOnce.Do(func() { close(call.terminalDone) })
	if mediaID != "" {
		s.media.Remove(mediaID)
	}
	s.persist(record)
	s.publish("call_ended", call)
	if s.notifier != nil {
		go s.notifier.NotifyCallResult(deviceID, peer, direction, status, event.Reason, endedAt)
	}
}

func terminalStatus(call *activeCall, event voicehost.CallEvent) string {
	if call.view.AnsweredAt != nil || call.view.Status == StatusConnected {
		return StatusCompleted
	}
	if call.view.Direction != "inbound" {
		return StatusFailed
	}
	if call.userRejected {
		return StatusRejected
	}
	reason := strings.ToLower(event.Reason)
	if event.Type == "CallCanceled" || strings.Contains(reason, "timed out") || strings.Contains(reason, "timeout") {
		return StatusMissed
	}
	return StatusFailed
}

func callDurationSeconds(record CallRecord, endedAt time.Time) int64 {
	start := record.StartedAt
	if record.AnsweredAt != nil {
		start = *record.AnsweredAt
	}
	if start.IsZero() || endedAt.Before(start) {
		return 0
	}
	return int64(endedAt.Sub(start) / time.Second)
}

func (s *Service) recordBusyResult(event voicehost.CallEvent, responseErr error) {
	s.mu.Lock()
	if _, duplicate := s.terminalSeen[event.CallID]; duplicate {
		s.mu.Unlock()
		return
	}
	s.terminalSeen[event.CallID] = struct{}{}
	s.mu.Unlock()
	at := event.Time
	if at.IsZero() {
		at = time.Now()
	}
	status, reason := StatusBusy, "device_busy"
	if responseErr != nil {
		status, reason = StatusFailed, responseErr.Error()
	}
	record := CallRecord{
		CallID: event.CallID, DeviceID: event.DeviceID, ICCID: s.iccid(event.DeviceID),
		Direction: "inbound", Peer: event.Caller, Status: status,
		StartedAt: at, EndedAt: &at, EndReason: reason,
	}
	s.persist(record)
	call := &activeCall{view: CallView{
		CallID: event.CallID, DeviceID: event.DeviceID, Direction: "inbound",
		Peer: event.Caller, Status: status, StartedAt: at, EndedAt: &at, EndReason: reason,
	}, record: record, terminal: true, terminalDone: make(chan struct{})}
	call.terminalOnce.Do(func() { close(call.terminalDone) })
	s.publish("call_ended", call)
	if s.notifier != nil {
		go s.notifier.NotifyCallResult(event.DeviceID, event.Caller, "inbound", status, reason, at)
	}
}

func (s *Service) publishMediaUpdate(event voicehost.CallEvent) {
	s.mu.RLock()
	call := s.calls[event.CallID]
	s.mu.RUnlock()
	if call != nil && !call.terminal {
		s.publish("media_updated", call)
	}
}
