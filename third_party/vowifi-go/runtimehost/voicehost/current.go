package voicehost

import (
	"context"
	"errors"
	"strings"
	"time"
)

type voiceAgent interface {
	DialContext(context.Context, string) (interface{}, error)
	HangupContext(context.Context, string) error
	Ready() bool
	Start() error
	Stop() error
}

type timedCallAgent interface {
	SimulateCall(context.Context, SimulateCallRequest) (SimulateCallResult, error)
}

// SetAgent retains the additive generic-agent registration surface.
func (g *Gateway) SetAgent(deviceID string, agent voiceAgent) error {
	if g == nil {
		return errors.New("voicehost: nil gateway")
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" || agent == nil {
		return errors.New("voicehost: device and agent are required")
	}
	g.bindIncomingHandlerCurrent(agent)
	g.mu.RLock()
	started, previous := g.started, g.agents[deviceID]
	g.mu.RUnlock()
	if started {
		if err := agent.Start(); err != nil {
			return err
		}
	}
	if previous != nil {
		if err := previous.Stop(); err != nil {
			if started {
				_ = agent.Stop()
			}
			return err
		}
	}
	g.mu.Lock()
	if g.agents == nil {
		g.agents = make(map[string]voiceAgent)
	}
	g.agents[deviceID] = agent
	g.mu.Unlock()
	return nil
}

// RemoveAgent detaches both recovered and additive registry entries.
func (g *Gateway) RemoveAgent(deviceID string) error {
	if g == nil {
		return nil
	}
	deviceID = strings.TrimSpace(deviceID)
	g.DetachDeviceCurrent(deviceID)
	g.mu.Lock()
	agent := g.agents[deviceID]
	delete(g.agents, deviceID)
	g.mu.Unlock()
	if agent == nil {
		return nil
	}
	return agent.Stop()
}

// GetAgentCurrent retains access to an additive generic agent.
func (g *Gateway) GetAgentCurrent(deviceID string) interface{} {
	if g == nil {
		return nil
	}
	if agent := g.internalAgent(deviceID); agent != nil {
		return agent
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.agents[strings.TrimSpace(deviceID)]
}

// DeviceStatusCurrent retains the displaced flat readiness projection.
func (g *Gateway) DeviceStatusCurrent(deviceID string) map[string]interface{} {
	deviceID = strings.TrimSpace(deviceID)
	if agent := g.internalAgent(deviceID); agent != nil {
		return map[string]interface{}{"device_id": deviceID, "ready": agent.Ready()}
	}
	g.mu.RLock()
	agent := g.agents[deviceID]
	g.mu.RUnlock()
	return map[string]interface{}{"device_id": deviceID, "ready": agent != nil && agent.Ready()}
}

// SimulateCallCurrent retains the displaced value-returning API.
func (g *Gateway) SimulateCallCurrent(
	ctx context.Context,
	deviceID string,
	request SimulateCallRequest,
) (SimulateCallResult, error) {
	result, err := g.SimulateCall(ctx, deviceID, request)
	if result == nil {
		return SimulateCallResult{}, err
	}
	return *result, err
}

func (g *Gateway) simulateCallWithCurrentAgent(
	ctx context.Context,
	deviceID string,
	request SimulateCallRequest,
) (*SimulateCallResult, error) {
	if g == nil {
		return nil, nil
	}
	g.mu.RLock()
	agent := g.agents[strings.TrimSpace(deviceID)]
	g.mu.RUnlock()
	if agent == nil {
		return nil, errors.New("voicehost: no agent for device " + deviceID)
	}
	if timed, ok := agent.(timedCallAgent); ok {
		result, err := timed.SimulateCall(ctx, request)
		return &result, err
	}
	return simulateGenericAgent(ctx, agent, request)
}

func simulateGenericAgent(
	ctx context.Context,
	agent voiceAgent,
	request SimulateCallRequest,
) (*SimulateCallResult, error) {
	if strings.TrimSpace(request.Callee) == "" {
		return nil, errors.New("voicehost: callee is empty")
	}
	call, err := agent.DialContext(ctx, request.Callee)
	if err != nil {
		return &SimulateCallResult{Reason: err.Error()}, err
	}
	if request.OnConnected != nil {
		request.OnConnected()
	}
	hold := normalizedCurrentHold(request.HoldSeconds)
	return waitCurrentCall(ctx, agent, call, hold)
}

func normalizedCurrentHold(hold int) int {
	if hold <= 0 {
		return DefaultSimulateCallHoldSeconds
	}
	if hold > MaxSimulateCallHoldSeconds {
		return MaxSimulateCallHoldSeconds
	}
	return hold
}

func waitCurrentCall(
	ctx context.Context,
	agent voiceAgent,
	call interface{},
	hold int,
) (*SimulateCallResult, error) {
	timer := time.NewTimer(time.Duration(hold) * time.Second)
	defer timer.Stop()
	callID := callIdentifier(call)
	select {
	case <-ctx.Done():
		err := errors.Join(ctx.Err(), hangupCurrentAgent(agent, callID))
		return &SimulateCallResult{Reason: err.Error()}, err
	case mediaErr := <-callMediaErrors(call):
		err := errors.Join(mediaErr, hangupCurrentAgent(agent, callID))
		return &SimulateCallResult{Reason: err.Error()}, err
	case <-timer.C:
		if err := hangupCurrentAgent(agent, callID); err != nil {
			return &SimulateCallResult{Reason: err.Error()}, err
		}
		return &SimulateCallResult{
			Success: true, DurationMs: int64(hold) * 1000, Message: "call completed",
		}, nil
	}
}

func callIdentifier(call interface{}) string {
	if identified, ok := call.(interface{ CallID() string }); ok {
		return identified.CallID()
	}
	return ""
}

func callMediaErrors(call interface{}) <-chan error {
	if mediaCall, ok := call.(interface{ MediaErrors() <-chan error }); ok {
		return mediaCall.MediaErrors()
	}
	return nil
}

func hangupCurrentAgent(agent voiceAgent, callID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return agent.HangupContext(ctx, callID)
}
