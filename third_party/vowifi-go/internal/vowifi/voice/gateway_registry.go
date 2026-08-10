package voice

import (
	"errors"
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

// RegisterDevice attaches an already-created IMS endpoint to the gateway.
func (g *Gateway) RegisterDevice(deviceID string, endpoint imsendpoint.Endpoint) error {
	deviceID = strings.TrimSpace(deviceID)
	if g == nil {
		return errors.New("voice: nil gateway")
	}
	if deviceID == "" {
		return errors.New("voice: device ID is empty")
	}
	if endpoint == nil {
		return fmt.Errorf("voice: IMS endpoint is nil for %s", deviceID)
	}
	if endpointID := strings.TrimSpace(endpoint.DeviceID()); endpointID != "" && endpointID != deviceID {
		return fmt.Errorf("voice: endpoint device %s does not match %s", endpointID, deviceID)
	}
	g.mu.Lock()
	if !g.running {
		g.mu.Unlock()
		return errors.New("voice: gateway is not running")
	}
	g.ensureMapsLocked()
	existing := g.agents[deviceID]
	epoch := g.epoch
	ctx := g.ctx
	adapter := g.clientAdapter
	g.mu.Unlock()
	if existing != nil {
		return g.replaceDeviceProvider(deviceID, existing, endpoint)
	}
	agent := NewAgent(deviceID, endpoint, g)
	agent.SetNotifier(g.forwardAgentEvent)
	if adapter != nil {
		agent.SetClientAdapter(adapter)
	}
	if err := agent.Start(ctx); err != nil {
		_ = agent.Stop()
		return err
	}
	g.mu.Lock()
	if !g.running || g.epoch != epoch {
		g.mu.Unlock()
		_ = agent.Stop()
		return errors.New("voice: gateway changed while registering device")
	}
	if g.agents[deviceID] != nil {
		g.mu.Unlock()
		_ = agent.Stop()
		return fmt.Errorf("voice: device %s was concurrently registered", deviceID)
	}
	g.agents[deviceID] = agent
	g.startEntryWorkerLocked(deviceID, agent)
	g.mu.Unlock()
	return nil
}

func (g *Gateway) replaceDeviceProvider(
	deviceID string,
	agent *Agent,
	endpoint imsendpoint.Endpoint,
) error {
	if agent.IsBusy() {
		return fmt.Errorf("voice: device %s has an active call", deviceID)
	}
	return agent.ReplaceIMSProvider(endpoint)
}

// UnregisterDevice removes a device and stops all of its bridge activity.
func (g *Gateway) UnregisterDevice(deviceID string) {
	if g == nil {
		return
	}
	deviceID = strings.TrimSpace(deviceID)
	g.mu.Lock()
	g.epoch++
	agent := g.agents[deviceID]
	worker := g.entryWorkers[deviceID]
	delete(g.agents, deviceID)
	delete(g.entryWorkers, deviceID)
	g.mu.Unlock()
	if worker != nil {
		worker.cancel()
	}
	if agent != nil {
		_ = agent.Stop()
	}
}

// DeviceStatus returns the recovered device envelope.
func (g *Gateway) DeviceStatus(deviceID string) map[string]interface{} {
	agent := g.GetAgent(deviceID)
	state := interface{}(map[string]interface{}{"ready": false})
	if agent != nil {
		state = agent.Snapshot()
	}
	return map[string]interface{}{"device_id": strings.TrimSpace(deviceID), "state": state}
}

// DeviceStatusCurrent retains the prior flat status projection.
func (g *Gateway) DeviceStatusCurrent(deviceID string) map[string]interface{} {
	if agent := g.GetAgent(deviceID); agent != nil {
		return agent.deviceStatus()
	}
	return map[string]interface{}{"registered": false, "device_id": strings.TrimSpace(deviceID)}
}
