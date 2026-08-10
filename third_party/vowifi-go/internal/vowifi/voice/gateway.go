package voice

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voiceclient"
)

// NewGateway retains the additive seeded-gateway constructor. The original
// Gateway remains usable as a zero value.
func NewGateway(agent *Agent) *Gateway {
	gateway := &Gateway{}
	gateway.ensureMapsLocked()
	if agent != nil {
		gateway.agents[agent.DeviceID()] = agent
		agent.setGateway(gateway)
	}
	return gateway
}

// Start starts the device registry and its per-device dispatch workers.
func (g *Gateway) Start(ctx context.Context) error {
	if g == nil {
		return errors.New("voice: nil gateway")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	g.mu.Lock()
	if g.cancel != nil {
		g.cancel()
	}
	g.ctx, g.cancel = context.WithCancel(ctx)
	g.running = true
	g.epoch++
	g.ensureMapsLocked()
	agents := make([]*Agent, 0, len(g.agents))
	for deviceID, agent := range g.agents {
		agents = append(agents, agent)
		g.startEntryWorkerLocked(deviceID, agent)
	}
	g.mu.Unlock()
	for _, agent := range agents {
		if err := g.startAgent(agent); err != nil {
			_ = g.Stop()
			return err
		}
	}
	return nil
}

// StartCurrent retains the displaced context-free start API.
func (g *Gateway) StartCurrent() error {
	return g.Start(context.Background())
}

// Stop cancels every worker before stopping and removing every Agent.
func (g *Gateway) Stop() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	if g.cancel != nil {
		g.cancel()
	}
	agents := make([]*Agent, 0, len(g.agents))
	for _, agent := range g.agents {
		agents = append(agents, agent)
	}
	workers := make([]*gatewayEntryWorker, 0, len(g.entryWorkers))
	for _, worker := range g.entryWorkers {
		workers = append(workers, worker)
	}
	g.agents = make(map[string]*Agent)
	g.entryWorkers = make(map[string]*gatewayEntryWorker)
	g.running = false
	g.epoch++
	g.ctx = nil
	g.cancel = nil
	g.mu.Unlock()
	for _, worker := range workers {
		stopGatewayEntryWorkerChain(worker)
	}
	var stopErr error
	for _, agent := range agents {
		stopErr = errors.Join(stopErr, agent.Stop())
	}
	return stopErr
}

// GetAgent returns the Agent registered for deviceID.
func (g *Gateway) GetAgent(deviceID string) *Agent {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.agents[strings.TrimSpace(deviceID)]
}

// SetNotifier installs the original incoming-call notifier.
func (g *Gateway) SetNotifier(notifier CallNotifier) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.notifier = normalizeCallNotifier(notifier)
	g.mu.Unlock()
}

// GetNotifier returns the original incoming-call notifier.
func (g *Gateway) GetNotifier() CallNotifier {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return normalizeCallNotifier(g.notifier)
}

func normalizeCallNotifier(notifier CallNotifier) CallNotifier {
	if notifier == nil {
		return nil
	}
	value := reflect.ValueOf(notifier)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil
		}
	}
	return notifier
}

// SetEventDispatcher installs the structured event sink.
func (g *Gateway) SetEventDispatcher(dispatcher events.EventDispatcher) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.eventDispatcher = dispatcher
	g.mu.Unlock()
}

// SetClientAdapter installs the local SIP/RTP adapter for present and future Agents.
func (g *Gateway) SetClientAdapter(adapter voiceclient.Adapter) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.clientAdapter = adapter
	agents := make([]*Agent, 0, len(g.agents))
	for _, agent := range g.agents {
		agents = append(agents, agent)
	}
	g.mu.Unlock()
	for _, agent := range agents {
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

func (g *Gateway) ensureMapsLocked() {
	if g.agents == nil {
		g.agents = make(map[string]*Agent)
	}
	if g.entryWorkers == nil {
		g.entryWorkers = make(map[string]*gatewayEntryWorker)
	}
}

func (g *Gateway) startAgent(agent *Agent) error {
	if agent == nil {
		return errors.New("voice: nil agent")
	}
	g.mu.RLock()
	ctx, adapter := g.ctx, g.clientAdapter
	g.mu.RUnlock()
	agent.setGateway(g)
	agent.SetNotifier(g.forwardAgentEvent)
	if adapter != nil {
		agent.SetClientAdapter(adapter)
	}
	return agent.Start(ctx)
}

func (g *Gateway) forwardAgentEvent(event events.Event) {
	g.dispatchEvent(context.Background(), event)
	incoming, ok := event.(events.EventIncomingCall)
	if !ok {
		if pointer, pointerOK := event.(*events.EventIncomingCall); pointerOK && pointer != nil {
			incoming = *pointer
			ok = true
		}
	}
	if !ok {
		return
	}
	if notifier := g.GetNotifier(); notifier != nil {
		notifier.NotifyIncomingCall(incoming.DevID, incoming.Caller, incoming.Callee)
	}
}

func (g *Gateway) dispatchEvent(ctx context.Context, event events.Event) {
	if g == nil || event == nil {
		return
	}
	g.mu.RLock()
	dispatcher := g.eventDispatcher
	g.mu.RUnlock()
	if dispatcher == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dispatcher.Dispatch(ctx, event)
}
