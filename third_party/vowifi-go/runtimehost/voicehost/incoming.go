package voicehost

import (
	"context"
	"errors"
	"strings"
	"time"
)

// IncomingCall describes a pending or renegotiated IMS call. OfferSDP points
// to the local RTP relay and is safe to pass to the client media engine.
type IncomingCall struct {
	DeviceID   string
	CallID     string
	Caller     string
	Callee     string
	OfferSDP   string
	ReceivedAt time.Time
	State      string
}

// AnswerRequest supplies the client media answer for an inbound call.
type AnswerRequest struct {
	DeviceID string
	CallID   string
	SDP      string
}

// AnswerResult describes a successfully established inbound call.
type AnswerResult struct {
	CallID   string
	OfferSDP string
	State    string
}

// RejectRequest selects a pending call and its SIP failure status.
type RejectRequest struct {
	DeviceID   string
	CallID     string
	StatusCode int
}

type incomingVoiceAgent interface {
	SetIncomingCallHandler(func(IncomingCall))
	IncomingCalls() []IncomingCall
	AnswerIncomingCall(context.Context, string, string) (AnswerResult, error)
	RejectIncomingCall(string, int) error
}

// SetIncomingCallHandler installs a callback for new calls and re-INVITE
// offers. Polling consumers can use IncomingCalls instead.
func (g *Gateway) SetIncomingCallHandler(handler func(IncomingCall)) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.incomingHandler = handler
	agents := make([]voiceAgent, 0, len(g.agents))
	for _, agent := range g.agents {
		agents = append(agents, agent)
	}
	g.mu.Unlock()
	for _, agent := range agents {
		g.bindIncomingHandler(agent)
	}
}

// IncomingCalls returns the active inbound calls for a device.
func (g *Gateway) IncomingCalls(deviceID string) ([]IncomingCall, error) {
	agent, err := g.incomingAgent(deviceID)
	if err != nil {
		return nil, err
	}
	return agent.IncomingCalls(), nil
}

// AnswerIncomingCall sends the client's SDP answer over the retained IMS
// INVITE transaction and starts the RTP relay.
func (g *Gateway) AnswerIncomingCall(ctx context.Context, request AnswerRequest) (AnswerResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return AnswerResult{}, ctx.Err()
	default:
	}
	if strings.TrimSpace(request.SDP) == "" {
		return AnswerResult{}, errors.New("voicehost: client SDP is required")
	}
	agent, err := g.incomingAgent(request.DeviceID)
	if err != nil {
		return AnswerResult{}, err
	}
	return agent.AnswerIncomingCall(ctx, request.CallID, request.SDP)
}

// RejectIncomingCall sends a final non-2xx response for a pending call.
func (g *Gateway) RejectIncomingCall(request RejectRequest) error {
	status := request.StatusCode
	if status == 0 {
		status = 486
	}
	agent, err := g.incomingAgent(request.DeviceID)
	if err != nil {
		return err
	}
	return agent.RejectIncomingCall(request.CallID, status)
}

func (g *Gateway) incomingAgent(deviceID string) (incomingVoiceAgent, error) {
	if g == nil {
		return nil, errors.New("voicehost: nil gateway")
	}
	g.mu.RLock()
	agent := g.agents[strings.TrimSpace(deviceID)]
	g.mu.RUnlock()
	incoming, ok := agent.(incomingVoiceAgent)
	if !ok {
		return nil, errors.New("voicehost: inbound voice is unavailable for device " + deviceID)
	}
	return incoming, nil
}

func (g *Gateway) bindIncomingHandler(agent voiceAgent) {
	incoming, ok := agent.(incomingVoiceAgent)
	if !ok {
		return
	}
	g.mu.RLock()
	handler := g.incomingHandler
	g.mu.RUnlock()
	incoming.SetIncomingCallHandler(handler)
}
