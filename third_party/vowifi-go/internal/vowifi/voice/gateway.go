package voice

import (
	"errors"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
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

// SimulateCall preserves the legacy name while placing a real IMS call.
func (g *Gateway) SimulateCall(number string) (*Call, error) {
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
