package voicehost

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/voice"
)

// BeginCallRequest starts a browser-media-backed outbound call.
type BeginCallRequest struct {
	DeviceID        string
	Callee          string
	SDP             string
	CaptureBasePath string
}

// CallSnapshot is a point-in-time host view of an active voice call.
type CallSnapshot struct {
	CallID    string        `json:"call_id"`
	DeviceID  string        `json:"device_id"`
	State     string        `json:"state"`
	Direction string        `json:"direction"`
	Peer      string        `json:"peer"`
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time,omitempty"`
	Duration  time.Duration `json:"duration"`
	ClientSDP string        `json:"client_sdp,omitempty"`
}

// BeginCall returns the real Call-ID immediately and reports later outcomes
// through SubscribeCallEvents.
func (g *Gateway) BeginCall(ctx context.Context, request BeginCallRequest) (CallSnapshot, error) {
	if g == nil {
		return CallSnapshot{}, errors.New("voicehost: nil gateway")
	}
	deviceID := strings.TrimSpace(request.DeviceID)
	agent := g.internalAgent(deviceID)
	if agent == nil {
		return CallSnapshot{}, errors.New("voicehost: browser calling is unavailable for device " + deviceID)
	}
	call, err := agent.BeginDial(ctx, request.Callee, request.SDP, request.CaptureBasePath)
	if err != nil {
		return CallSnapshot{}, err
	}
	return callSnapshotFromVoice(deviceID, call.Snapshot()), nil
}

// ActiveCall returns the current call for one device.
func (g *Gateway) ActiveCall(deviceID string) *CallSnapshot {
	agent := g.internalAgent(deviceID)
	if agent == nil {
		return nil
	}
	snapshot := agent.SnapshotCurrent()
	if snapshot == nil || snapshot.ActiveCall == nil {
		return nil
	}
	result := callSnapshotFromVoice(strings.TrimSpace(deviceID), snapshot.ActiveCall)
	return &result
}

// HangupCall sends CANCEL before answer and BYE after answer.
func (g *Gateway) HangupCall(ctx context.Context, deviceID, callID string) error {
	agent := g.internalAgent(deviceID)
	if agent == nil {
		return errors.New("voicehost: voice is unavailable for device " + strings.TrimSpace(deviceID))
	}
	return agent.HangupContext(ctx, strings.TrimSpace(callID))
}

// SendCallDTMF sends one negotiated RFC 4733 event on a device call.
func (g *Gateway) SendCallDTMF(deviceID, callID, digit string) error {
	agent := g.internalAgent(deviceID)
	if agent == nil {
		return errors.New("voicehost: voice is unavailable for device " + strings.TrimSpace(deviceID))
	}
	return agent.SendDTMF(strings.TrimSpace(callID), digit)
}

// StartCallCapture enables automatic paired PCAP/audio capture.
func (g *Gateway) StartCallCapture(deviceID, callID, basePath string) error {
	agent := g.internalAgent(deviceID)
	if agent == nil {
		return errors.New("voicehost: voice is unavailable for device " + strings.TrimSpace(deviceID))
	}
	return agent.StartCallCapture(strings.TrimSpace(callID), basePath)
}

func callSnapshotFromVoice(deviceID string, snapshot *voice.CallSnapshot) CallSnapshot {
	if snapshot == nil {
		return CallSnapshot{DeviceID: deviceID}
	}
	return CallSnapshot{
		CallID: snapshot.CallID, DeviceID: deviceID, State: snapshot.State,
		Direction: snapshot.Direction, Peer: snapshot.Peer, StartTime: snapshot.StartTime,
		EndTime: snapshot.EndTime, Duration: snapshot.Duration, ClientSDP: snapshot.ClientSDP,
	}
}
