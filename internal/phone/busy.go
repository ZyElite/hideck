package phone

import (
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

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
	}, record: record, terminal: true,
		terminalDone: make(chan struct{}), finalizedDone: make(chan struct{})}
	call.terminalOnce.Do(func() { close(call.terminalDone) })
	call.finalizedOnce.Do(func() { close(call.finalizedDone) })
	s.publish("call_ended", call)
	if s.notifier != nil {
		go s.notifier.NotifyCallResult(event.DeviceID, event.Caller, "inbound", status, reason, at)
	}
}
