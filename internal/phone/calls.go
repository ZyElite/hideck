package phone

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

var calleePattern = regexp.MustCompile(`^\+?[0-9]{1,32}$`)
var dtmfPattern = regexp.MustCompile(`^[0-9*#]$`)

func (s *Service) StartCall(request StartCallRequest) (CallView, error) {
	request.DeviceID, request.Callee = strings.TrimSpace(request.DeviceID), strings.TrimSpace(request.Callee)
	if request.DeviceID == "" || !calleePattern.MatchString(request.Callee) {
		return CallView{}, errors.New("phone: device and a valid callee are required")
	}
	media, err := s.controlMedia(request.Owner, request.MediaID, request.Lease)
	if err != nil {
		return CallView{}, err
	}
	if err := s.reserveDevice(request.DeviceID); err != nil {
		return CallView{}, err
	}
	startedAt := time.Now()
	snapshot, err := s.gateway.BeginCall(s.ctx, voicehost.BeginCallRequest{
		DeviceID: request.DeviceID, Callee: request.Callee, SDP: media.PlainSDP(),
		CaptureBasePath: s.captureBase(request.DeviceID, startedAt),
	})
	if err != nil {
		s.releaseDeviceReservation(request.DeviceID)
		return CallView{}, err
	}
	call := s.newOutboundCall(request, snapshot, startedAt)
	s.mu.Lock()
	s.calls[call.view.CallID] = call
	s.deviceCalls[call.view.DeviceID] = call.view.CallID
	pendingMediaDrop := s.bindMediaLocked(call.view.CallID, call.mediaID)
	pendingEvents := s.pendingEvents[call.view.CallID]
	delete(s.pendingEvents, call.view.CallID)
	startedView := call.snapshot(request.Lease)
	s.mu.Unlock()
	s.resumePendingMediaDrop(call.mediaID, pendingMediaDrop)
	s.persist(call.record)
	s.publish("call_started", call)
	for _, event := range pendingEvents {
		s.dispatchCallEvent(event)
	}
	return startedView, nil
}

func (s *Service) newOutboundCall(
	request StartCallRequest,
	snapshot voicehost.CallSnapshot,
	startedAt time.Time,
) *activeCall {
	view := CallView{
		CallID: snapshot.CallID, DeviceID: request.DeviceID, Direction: "outbound",
		Peer: request.Callee, Status: StatusCalling, MediaID: request.MediaID, StartedAt: startedAt,
	}
	record := CallRecord{
		CallID: snapshot.CallID, DeviceID: request.DeviceID, ICCID: s.iccid(request.DeviceID),
		Direction: "outbound", Peer: request.Callee, Status: StatusCalling, StartedAt: startedAt,
	}
	return &activeCall{
		view: view, record: record, owner: request.Owner, lease: request.Lease,
		mediaID: request.MediaID, recordingBase: s.captureBase(request.DeviceID, startedAt),
		terminalDone: make(chan struct{}), finalizedDone: make(chan struct{}),
	}
}

func (s *Service) Answer(ctx context.Context, request ControlRequest) (CallView, error) {
	call, media, err := s.claimIncoming(request)
	if err != nil {
		return CallView{}, err
	}
	s.mu.RLock()
	incomingSDP, deviceID, callID := call.incomingSDP, call.view.DeviceID, call.view.CallID
	s.mu.RUnlock()
	if err := media.Attach(incomingSDP); err != nil {
		rejectErr := s.gateway.RejectIncomingCall(voicehost.RejectRequest{
			DeviceID: deviceID, CallID: callID, StatusCode: 488,
		})
		return CallView{}, errors.Join(err, rejectErr)
	}
	_, err = s.gateway.AnswerIncomingCall(ctx, voicehost.AnswerRequest{
		DeviceID: deviceID, CallID: callID, SDP: media.PlainSDP(),
	})
	if err != nil {
		return CallView{}, err
	}
	s.assignControl(call.view.CallID, request.Owner, request.MediaID, request.Lease)
	s.startMixedRecording(call, media)
	return s.callView(callID, request.Lease), nil
}

func (s *Service) Reject(request ControlRequest) error {
	call, _, err := s.claimIncoming(request)
	if err != nil {
		return err
	}
	s.mu.Lock()
	call.userRejected = true
	call.owner, call.lease, call.mediaID = request.Owner, request.Lease, request.MediaID
	call.view.MediaID = request.MediaID
	pendingMediaDrop := s.bindMediaLocked(request.CallID, request.MediaID)
	deviceID := call.view.DeviceID
	s.mu.Unlock()
	s.resumePendingMediaDrop(request.MediaID, pendingMediaDrop)
	return s.gateway.RejectIncomingCall(voicehost.RejectRequest{
		DeviceID: deviceID, CallID: request.CallID, StatusCode: 486,
	})
}

func (s *Service) Hangup(ctx context.Context, owner, callID, lease string) error {
	call, err := s.controlledCall(owner, callID, lease)
	if err != nil {
		return err
	}
	s.mu.RLock()
	deviceID, resolvedCallID := call.view.DeviceID, call.view.CallID
	s.mu.RUnlock()
	return s.gateway.HangupCall(ctx, deviceID, resolvedCallID)
}

func (s *Service) DTMF(owner, callID, lease, digit string) error {
	if !dtmfPattern.MatchString(digit) {
		return errors.New("phone: DTMF must be one of 0-9, * or #")
	}
	call, err := s.controlledCall(owner, callID, lease)
	if err != nil {
		return err
	}
	s.mu.RLock()
	status, deviceID, resolvedCallID := call.view.Status, call.view.DeviceID, call.view.CallID
	s.mu.RUnlock()
	if status != StatusConnected {
		return errors.New("phone: DTMF requires a connected call")
	}
	return s.gateway.SendCallDTMF(deviceID, resolvedCallID, digit)
}

func (s *Service) RefreshMedia(request RefreshRequest) (CallView, string, error) {
	media := s.media.Get(request.MediaID)
	if media == nil || media.Owner != request.Owner {
		return CallView{}, "", errors.New("phone: media session is unavailable")
	}
	s.mu.RLock()
	call := s.calls[request.CallID]
	if call == nil || call.terminal {
		s.mu.RUnlock()
		return CallView{}, "", errors.New("phone: active call not found")
	}
	if !request.Takeover && !secureEqual(call.lease, request.Lease) {
		s.mu.RUnlock()
		return CallView{}, "", errors.New("phone: control lease does not own this call")
	}
	s.mu.RUnlock()
	if err := s.attachCurrentMedia(request.CallID, media); err != nil {
		return CallView{}, "", err
	}
	s.mu.RLock()
	callForRecorder := s.calls[request.CallID]
	s.mu.RUnlock()
	s.attachMixedRecorder(callForRecorder, media)
	s.mu.Lock()
	call = s.calls[request.CallID]
	if call == nil || call.terminal {
		s.mu.Unlock()
		return CallView{}, "", errors.New("phone: active call ended while refreshing media")
	}
	if !request.Takeover && !secureEqual(call.lease, request.Lease) {
		s.mu.Unlock()
		return CallView{}, "", errors.New("phone: call control changed while refreshing media")
	}
	oldMediaID := call.mediaID
	call.owner, call.lease, call.mediaID = request.Owner, media.Lease, request.MediaID
	call.view.MediaID = request.MediaID
	delete(s.mediaCalls, oldMediaID)
	delete(s.pendingMediaDrops, oldMediaID)
	pendingMediaDrop := s.bindMediaLocked(request.CallID, request.MediaID)
	if call.disconnectTimer != nil {
		call.disconnectTimer.Stop()
		call.disconnectTimer = nil
	}
	s.mu.Unlock()
	s.resumePendingMediaDrop(request.MediaID, pendingMediaDrop)
	if oldMediaID != "" && oldMediaID != request.MediaID {
		s.media.Remove(oldMediaID)
	}
	s.publish("media_recovered", call)
	return s.callView(request.CallID, media.Lease), media.Lease, nil
}

func (s *Service) attachCurrentMedia(callID string, media *MediaSession) error {
	s.mu.RLock()
	call := s.calls[callID]
	if call == nil {
		s.mu.RUnlock()
		return errors.New("phone: active call not found")
	}
	remoteSDP, deviceID := call.incomingSDP, call.view.DeviceID
	s.mu.RUnlock()
	if snapshot := s.gateway.ActiveCall(deviceID); snapshot != nil && snapshot.ClientSDP != "" {
		remoteSDP = snapshot.ClientSDP
	}
	if remoteSDP == "" {
		return errors.New("phone: call media endpoint is not negotiated yet")
	}
	return media.Attach(remoteSDP)
}

func (s *Service) claimIncoming(request ControlRequest) (*activeCall, *MediaSession, error) {
	media, err := s.controlMedia(request.Owner, request.MediaID, request.Lease)
	if err != nil {
		return nil, nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	call := s.calls[request.CallID]
	if call == nil || call.terminal || call.view.Direction != "inbound" {
		return nil, nil, errors.New("phone: pending incoming call not found")
	}
	if call.lease != "" && (!secureEqual(call.lease, request.Lease) || call.owner != request.Owner) {
		return nil, nil, errors.New("phone: call is controlled by another browser")
	}
	return call, media, nil
}

func (s *Service) controlMedia(owner, mediaID, lease string) (*MediaSession, error) {
	media := s.media.Get(strings.TrimSpace(mediaID))
	if media == nil || !media.Matches(owner, lease) {
		return nil, errors.New("phone: invalid media control lease")
	}
	return media, nil
}

func (s *Service) controlledCall(owner, callID, lease string) (*activeCall, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	call := s.calls[strings.TrimSpace(callID)]
	if call == nil || call.terminal {
		return nil, errors.New("phone: active call not found")
	}
	if call.owner != owner || !secureEqual(call.lease, lease) {
		return nil, errors.New("phone: control lease does not own this call")
	}
	return call, nil
}

func (s *Service) reserveDevice(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, busy := s.deviceCalls[deviceID]; busy {
		return fmt.Errorf("phone: device %s already has an active call", deviceID)
	}
	s.deviceCalls[deviceID] = ""
	return nil
}

func (s *Service) releaseDeviceReservation(deviceID string) {
	s.mu.Lock()
	if s.deviceCalls[deviceID] == "" {
		delete(s.deviceCalls, deviceID)
	}
	s.mu.Unlock()
}

func (s *Service) assignControl(callID, owner, mediaID, lease string) {
	s.mu.Lock()
	call := s.calls[callID]
	if call == nil {
		s.mu.Unlock()
		return
	}
	call.owner, call.lease, call.mediaID = owner, lease, mediaID
	call.view.MediaID = mediaID
	pendingMediaDrop := s.bindMediaLocked(callID, mediaID)
	s.mu.Unlock()
	s.resumePendingMediaDrop(mediaID, pendingMediaDrop)
}

func (s *Service) iccid(deviceID string) string {
	if s.resolveICCID == nil {
		return ""
	}
	return strings.TrimSpace(s.resolveICCID(deviceID))
}
