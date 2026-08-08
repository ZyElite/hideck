// Package voicehost defines the voice gateway surface used to drive VoWiFi
// calls.
//
// Reconstructed from the decompiled engine/runtimehost/voicehost.
package voicehost

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// voiceAgent is the agent surface the gateway drives. It is implemented by
// the voice package's Agent; the interface avoids an import cycle.
type voiceAgent interface {
	DialContext(context.Context, string) (interface{}, error)
	HangupContext(context.Context, string) error
	Ready() bool
	Start() error
	Stop() error
}

// DefaultSimulateCallHoldSeconds is the default hold time for the legacy call command.
const DefaultSimulateCallHoldSeconds = 10

// MaxSimulateCallHoldSeconds bounds the legacy call command hold time.
const MaxSimulateCallHoldSeconds = 60

// SimulateCallRequest describes the legacy timed VoWiFi call command.
type SimulateCallRequest struct {
	Callee      string
	HoldSeconds int
	OnConnected func()
}

// SimulateCallResult is the outcome of the legacy timed call command.
type SimulateCallResult struct {
	Success    bool
	Message    string
	DurationMs int64
	Reason     string
}

// Notifier receives voice events.
type Notifier interface{}

// Profile is the voice profile of a device.
type Profile struct {
	DeviceID string
	IMSI     string
	IMPI     string
	Domain   string
}

// Gateway drives VoWiFi calls on a device.
type Gateway struct {
	mu              sync.RWMutex
	notifier        Notifier
	client          ClientAdapter
	dispatcher      interface{ Dispatch(interface{}) }
	agents          map[string]voiceAgent
	incomingHandler func(IncomingCall)
	started         bool
}

// NewGateway returns a Gateway.
func NewGateway() *Gateway {
	return &Gateway{agents: make(map[string]voiceAgent)}
}

// Start starts the voice gateway.
func (g *Gateway) Start(ctx context.Context) error {
	if g == nil {
		return errors.New("voicehost: nil gateway")
	}
	g.mu.Lock()
	if g.started {
		g.mu.Unlock()
		return nil
	}
	g.started = true
	agents := make([]voiceAgent, 0, len(g.agents))
	for _, a := range g.agents {
		agents = append(agents, a)
	}
	g.mu.Unlock()
	startedAgents := make([]voiceAgent, 0, len(agents))
	for _, a := range agents {
		if err := a.Start(); err != nil {
			for _, startedAgent := range startedAgents {
				_ = startedAgent.Stop()
			}
			g.mu.Lock()
			g.started = false
			g.mu.Unlock()
			return err
		}
		startedAgents = append(startedAgents, a)
	}
	return nil
}

// Stop stops the voice gateway.
func (g *Gateway) Stop() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	if !g.started {
		g.mu.Unlock()
		return nil
	}
	g.started = false
	agents := make([]voiceAgent, 0, len(g.agents))
	for _, a := range g.agents {
		agents = append(agents, a)
	}
	g.mu.Unlock()
	var stopErr error
	for _, a := range agents {
		stopErr = errors.Join(stopErr, a.Stop())
	}
	return stopErr
}

// SetNotifier installs the event notifier.
func (g *Gateway) SetNotifier(n Notifier) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.notifier = n
	g.mu.Unlock()
}

// SetAgent registers a voice agent for a device.
func (g *Gateway) SetAgent(deviceID string, a voiceAgent) error {
	if g == nil {
		return errors.New("voicehost: nil gateway")
	}
	if strings.TrimSpace(deviceID) == "" || a == nil {
		return errors.New("voicehost: device and agent are required")
	}
	deviceID = strings.TrimSpace(deviceID)
	g.bindIncomingHandler(a)
	g.mu.RLock()
	started := g.started
	previous := g.agents[deviceID]
	g.mu.RUnlock()
	if started {
		if err := a.Start(); err != nil {
			return err
		}
	}
	if previous != nil {
		if err := previous.Stop(); err != nil {
			if started {
				_ = a.Stop()
			}
			return err
		}
	}
	g.mu.Lock()
	if g.agents == nil {
		g.agents = make(map[string]voiceAgent)
	}
	g.agents[deviceID] = a
	g.mu.Unlock()
	return nil
}

// RemoveAgent detaches and stops a device voice agent.
func (g *Gateway) RemoveAgent(deviceID string) error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	deviceID = strings.TrimSpace(deviceID)
	agent := g.agents[deviceID]
	delete(g.agents, deviceID)
	g.mu.Unlock()
	if agent == nil {
		return nil
	}
	return agent.Stop()
}

// SimulateCall preserves the command API while driving a real IMS call.
func (g *Gateway) SimulateCall(ctx context.Context, deviceID string, req SimulateCallRequest) (SimulateCallResult, error) {
	if g == nil {
		return SimulateCallResult{}, errors.New("voicehost: nil gateway")
	}
	g.mu.RLock()
	agent := g.agents[deviceID]
	g.mu.RUnlock()
	if agent == nil {
		return SimulateCallResult{}, errors.New("voicehost: no agent for device " + deviceID)
	}
	if strings.TrimSpace(req.Callee) == "" {
		return SimulateCallResult{}, errors.New("voicehost: callee is empty")
	}
	call, err := agent.DialContext(ctx, req.Callee)
	if err != nil {
		return SimulateCallResult{Success: false, Reason: err.Error()}, err
	}
	callID := ""
	if c, ok := call.(interface{ CallID() string }); ok {
		callID = c.CallID()
	}
	hold := req.HoldSeconds
	if hold <= 0 {
		hold = DefaultSimulateCallHoldSeconds
	}
	if hold > MaxSimulateCallHoldSeconds {
		hold = MaxSimulateCallHoldSeconds
	}
	if req.OnConnected != nil {
		req.OnConnected()
	}
	mediaErrors := callMediaErrors(call)
	timer := time.NewTimer(time.Duration(hold) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		resultErr := errors.Join(ctx.Err(), hangupAgent(agent, callID))
		return SimulateCallResult{Success: false, Reason: resultErr.Error()}, resultErr
	case mediaErr := <-mediaErrors:
		resultErr := errors.Join(mediaErr, hangupAgent(agent, callID))
		return SimulateCallResult{Success: false, Reason: resultErr.Error()}, resultErr
	case <-timer.C:
		err := hangupAgent(agent, callID)
		if err != nil {
			return SimulateCallResult{Success: false, Reason: err.Error()}, err
		}
		return SimulateCallResult{Success: true, Message: "call completed", DurationMs: int64(hold) * 1000}, nil
	}
}

func callMediaErrors(call interface{}) <-chan error {
	if mediaCall, ok := call.(interface{ MediaErrors() <-chan error }); ok {
		return mediaCall.MediaErrors()
	}
	return nil
}

func hangupAgent(agent voiceAgent, callID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return agent.HangupContext(ctx, callID)
}

// GetAgent returns the voice agent for a device.
func (g *Gateway) GetAgent(deviceID string) interface{} {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.agents[deviceID]
}

// DeviceStatus returns the voice status for a device.
func (g *Gateway) DeviceStatus(deviceID string) map[string]interface{} {
	g.mu.RLock()
	agent := g.agents[deviceID]
	g.mu.RUnlock()
	return map[string]interface{}{
		"device_id": deviceID,
		"ready":     agent != nil && agent.Ready(),
	}
}

// ClientAdapter is the client-facing adapter the gateway drives.
type ClientAdapter interface {
	HandleClientInvite(peer, sdp string) (interface{}, error)
	HandleClientBye(callID string) error
	HandleClientCancel(callID string) error
	HandleClientAck(callID string) error
	HandleClientPrack(callID string) error
}

// SetClientAdapter wires the client adapter.
func (g *Gateway) SetClientAdapter(a ClientAdapter) {
	if g == nil {
		return
	}
	g.client = a
}

// HandleClientInvite forwards a client INVITE to the adapter.
func (g *Gateway) HandleClientInvite(peer, sdp string) (interface{}, error) {
	if g == nil || g.client == nil {
		return nil, errors.New("voicehost: no client adapter")
	}
	return g.client.HandleClientInvite(peer, sdp)
}

// HandleClientBye forwards a client BYE.
func (g *Gateway) HandleClientBye(callID string) error {
	if g == nil || g.client == nil {
		return errors.New("voicehost: no client adapter")
	}
	return g.client.HandleClientBye(callID)
}

// HandleClientCancel forwards a client CANCEL.
func (g *Gateway) HandleClientCancel(callID string) error {
	if g == nil || g.client == nil {
		return errors.New("voicehost: no client adapter")
	}
	return g.client.HandleClientCancel(callID)
}

// HandleClientAck forwards a client ACK.
func (g *Gateway) HandleClientAck(callID string) error {
	if g == nil || g.client == nil {
		return errors.New("voicehost: no client adapter")
	}
	return g.client.HandleClientAck(callID)
}

// HandleClientPrack forwards a client PRACK.
func (g *Gateway) HandleClientPrack(callID string) error {
	if g == nil || g.client == nil {
		return errors.New("voicehost: no client adapter")
	}
	return g.client.HandleClientPrack(callID)
}

// SetEventDispatcher wires the event dispatcher.
func (g *Gateway) SetEventDispatcher(d interface{ Dispatch(interface{}) }) {
	if g == nil {
		return
	}
	g.dispatcher = d
}

// StartPCAP reports that the legacy API lacks the required output target.
func (g *Gateway) StartPCAP(deviceID string) error {
	if g == nil || g.GetAgent(deviceID) == nil {
		return errors.New("voicehost: no agent for device " + deviceID)
	}
	return errors.New("voicehost: packet capture output is not configured")
}

// StopPCAP reports that no capture can be addressed by the legacy API.
func (g *Gateway) StopPCAP(deviceID string) error {
	if g == nil || g.GetAgent(deviceID) == nil {
		return errors.New("voicehost: no agent for device " + deviceID)
	}
	return errors.New("voicehost: packet capture output is not configured")
}

// eventDispatcherAdapter adapts an event dispatcher.
type eventDispatcherAdapter struct {
	dispatch func(interface{})
}

// Dispatch forwards an event.
func (a *eventDispatcherAdapter) Dispatch(ev interface{}) {
	if a != nil && a.dispatch != nil {
		a.dispatch(ev)
	}
}

// lifecycleAdapter binds a device to the voice lifecycle.
type lifecycleAdapter struct {
	gateway *Gateway
}

// AttachDevice registers a device.
func (a *lifecycleAdapter) AttachDevice(deviceID string) error {
	if a == nil || a.gateway == nil {
		return errors.New("voicehost: no gateway")
	}
	if a.gateway.GetAgent(deviceID) == nil {
		return errors.New("voicehost: no agent for device " + deviceID)
	}
	return nil
}

// DetachDevice deregisters a device.
func (a *lifecycleAdapter) DetachDevice(deviceID string) error {
	if a == nil || a.gateway == nil {
		return errors.New("voicehost: no gateway")
	}
	return a.gateway.RemoveAgent(deviceID)
}
