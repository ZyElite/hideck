package voicehost

import (
	"errors"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

type runtimeLifecycle interface {
	AttachDevice(string, imsendpoint.Endpoint) error
	DetachDevice(string)
}

type lifecycleAdapter struct {
	gateway *Gateway
}

func (adapter lifecycleAdapter) AttachDevice(deviceID string, endpoint imsendpoint.Endpoint) error {
	if adapter.gateway == nil || adapter.gateway.inner == nil {
		return nil
	}
	return adapter.gateway.attachDevice(deviceID, endpoint)
}

func (adapter lifecycleAdapter) DetachDevice(deviceID string) {
	if adapter.gateway != nil {
		adapter.gateway.detachDevice(deviceID)
	}
}

// AttachDeviceCurrent exposes lifecycle binding to the current root host.
func (g *Gateway) AttachDeviceCurrent(deviceID string, endpoint imsendpoint.Endpoint) error {
	if g == nil || g.inner == nil {
		return errors.New("voicehost: no gateway")
	}
	return attachRuntimeLifecycle(&lifecycleAdapter{gateway: g}, deviceID, endpoint)
}

func attachRuntimeLifecycle(
	lifecycle runtimeLifecycle,
	deviceID string,
	endpoint imsendpoint.Endpoint,
) error {
	return lifecycle.AttachDevice(deviceID, endpoint)
}

func (g *Gateway) attachDevice(deviceID string, endpoint imsendpoint.Endpoint) error {
	deviceID = strings.TrimSpace(deviceID)
	if err := g.inner.RegisterDevice(deviceID, endpoint); err != nil {
		return err
	}
	g.mu.Lock()
	if g.innerDevices == nil {
		g.innerDevices = make(map[string]struct{})
	}
	g.innerDevices[deviceID] = struct{}{}
	handler := g.incomingHandler
	g.mu.Unlock()
	bindIncomingVoiceAgent(g.inner.GetAgent(deviceID), handler)
	return nil
}

// DetachDeviceCurrent removes a device from the recovered registry.
func (g *Gateway) DetachDeviceCurrent(deviceID string) {
	if g != nil {
		detachRuntimeLifecycle(&lifecycleAdapter{gateway: g}, deviceID)
	}
}

func detachRuntimeLifecycle(lifecycle runtimeLifecycle, deviceID string) {
	lifecycle.DetachDevice(deviceID)
}

func (g *Gateway) detachDevice(deviceID string) {
	deviceID = strings.TrimSpace(deviceID)
	if g.inner != nil {
		g.inner.UnregisterDevice(deviceID)
	}
	g.mu.Lock()
	delete(g.innerDevices, deviceID)
	g.mu.Unlock()
}

var _ runtimeLifecycle = lifecycleAdapter{}
var _ runtimeLifecycle = (*lifecycleAdapter)(nil)
