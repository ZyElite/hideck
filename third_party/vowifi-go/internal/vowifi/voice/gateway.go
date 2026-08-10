package voice

import (
	"context"
	"errors"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voiceclient"
)

// NewGateway creates a client-facing gateway.
func NewGateway(agent *Agent) *Gateway {
	return &Gateway{agent: agent}
}

// GetAgent returns the underlying agent.
func (g *Gateway) GetAgent() *Agent {
	if g == nil {
		return nil
	}
	return g.agent
}

// SetNotifier wires the event notifier.
func (g *Gateway) SetNotifier(fn func(events.Event)) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.notifier = fn
	g.mu.Unlock()
}

// GetNotifier returns the event notifier.
func (g *Gateway) GetNotifier() func(events.Event) {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.notifier
}

// SetClientAdapter installs the local SIP/RTP adapter and immediately
// projects it into the production agent owned by this gateway.
func (g *Gateway) SetClientAdapter(adapter voiceclient.Adapter) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.clientAdapter = adapter
	agent := g.agent
	g.mu.Unlock()
	if agent != nil {
		agent.SetClientAdapter(adapter)
	}
}

// GetClientAdapter returns the configured local client adapter.
func (g *Gateway) GetClientAdapter() voiceclient.Adapter {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.clientAdapter
}

// Start starts the gateway.
func (g *Gateway) Start() error {
	if g == nil {
		return errors.New("voice: nil gateway")
	}
	g.mu.Lock()
	if g.started {
		g.mu.Unlock()
		return nil
	}
	g.started = true
	g.mu.Unlock()
	if g.agent == nil {
		g.mu.Lock()
		g.started = false
		g.mu.Unlock()
		return errors.New("voice: no agent")
	}
	if err := g.agent.Start(); err != nil {
		g.mu.Lock()
		g.started = false
		g.mu.Unlock()
		return err
	}
	return nil
}

// Stop stops the gateway.
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
	g.mu.Unlock()
	if g.agent != nil {
		return g.agent.Stop()
	}
	return nil
}

// RegisterDevice registers the device with the IMS network.
func (g *Gateway) RegisterDevice() error {
	if g == nil || g.agent == nil {
		return errors.New("voice: no agent")
	}
	return g.agent.register()
}

// UnregisterDevice deregisters the device.
func (g *Gateway) UnregisterDevice() error {
	if g == nil || g.agent == nil {
		return errors.New("voice: no agent")
	}
	return g.agent.unregister()
}

// DeviceStatus returns the device registration status.
func (g *Gateway) DeviceStatus() map[string]interface{} {
	if g == nil || g.agent == nil {
		return map[string]interface{}{"registered": false}
	}
	return g.agent.deviceStatus()
}

// OnIMSInvite retains the v1.5.5 raw compatibility entry point. Production
// endpoint delivery uses Agent.OnIMSInvite with a retained server handle.
func (g *Gateway) OnIMSInvite(deviceID string, raw []byte, session *imsendpoint.Session) {
	if g == nil || g.agent == nil {
		logging.WarnRate("voice-gateway-invite:"+strings.TrimSpace(deviceID),
			voiceActorEventLogInterval, "voice gateway has no agent", "device", deviceID)
		return
	}
	if deviceID = strings.TrimSpace(deviceID); deviceID != "" && deviceID != g.agent.DeviceID() {
		logging.WarnRate("voice-gateway-invite:"+deviceID, voiceActorEventLogInterval,
			"voice gateway agent does not match device", "device", deviceID)
		return
	}
	request, err := parseVoiceRequest(string(raw))
	if err != nil {
		logging.WarnRate("voice-gateway-invite-parse:"+deviceID, voiceActorEventLogInterval,
			"voice gateway INVITE parse failed", "device", deviceID, "err", err)
		return
	}
	g.agent.OnIMSInvite(request, session, nil)
}

// HandleClientPrack routes a local PRACK to the matching device Agent.
func (g *Gateway) HandleClientPrack(
	deviceID string,
	request *sip.Request,
	transaction sip.ServerTransaction,
) {
	if g == nil || g.agent == nil ||
		(strings.TrimSpace(deviceID) != "" && strings.TrimSpace(deviceID) != g.agent.DeviceID()) {
		respondClientRequest(transaction, request, 481, "Call/Transaction Does Not Exist")
		return
	}
	g.agent.HandlePrack(request, transaction)
}

// SimulateCall runs the recovered device-scoped timed-call workflow.
func (g *Gateway) SimulateCall(
	ctx context.Context,
	deviceID string,
	request SimulateCallRequest,
) (*SimulateCallResult, error) {
	if g == nil || g.agent == nil {
		return nil, errors.New("voice: no agent")
	}
	if deviceID != "" && deviceID != g.agent.DeviceID() {
		return nil, errors.New("voice: agent not found for device " + deviceID)
	}
	return g.agent.SimulateCall(ctx, request)
}

// SimulateCallNumber retains the additive direct-dial convenience API.
func (g *Gateway) SimulateCallNumber(number string) (*Call, error) {
	if g == nil || g.agent == nil {
		return nil, errors.New("voice: no agent")
	}
	return g.agent.simulateCall(number)
}

// dispatchEvent forwards an event to the notifier.
func (g *Gateway) dispatchEvent(ev events.Event) {
	if g == nil {
		return
	}
	g.mu.RLock()
	fn := g.notifier
	g.mu.RUnlock()
	if fn != nil {
		fn(ev)
	}
}
