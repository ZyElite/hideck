package voice

import (
	"context"
	"errors"
	"strings"
)

// BeginDial starts a real outbound INVITE and returns its Call-ID before the
// final response arrives. Completion is reported through the existing events.
func (a *Agent) BeginDial(ctx context.Context, number, clientSDP, captureBasePath string) (*Call, error) {
	if err := a.validateAsyncDial(number); err != nil {
		return nil, err
	}
	call, err := a.startOutboundCall(strings.TrimSpace(number))
	if err != nil {
		return nil, err
	}
	call.setCaptureBasePath(captureBasePath)
	if ctx == nil {
		ctx = context.Background()
	}
	runtimeCtx, cancelRuntime := context.WithCancel(ctx)
	call.SetOutboundRuntimeCancel(cancelRuntime)
	go func() {
		defer cancelRuntime()
		_, _ = a.executeOutboundCall(runtimeCtx, call, clientSDP)
	}()
	return call, nil
}

func (a *Agent) validateAsyncDial(number string) error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	endpoint := a.imsEndpoint()
	if endpoint == nil {
		return errors.New("voice: no IMS service")
	}
	if !endpoint.IsRegistered() {
		return errors.New("voice: IMS not registered")
	}
	a.mu.RLock()
	started := a.started
	a.mu.RUnlock()
	if !started {
		return errors.New("voice: agent not started")
	}
	if strings.TrimSpace(number) == "" {
		return errors.New("voice: callee is empty")
	}
	return nil
}

// StartCallCapture enables paired PCAP and audio capture for one active call.
func (a *Agent) StartCallCapture(callID, basePath string) error {
	call := a.activeCallByHandle(callID)
	if call == nil {
		return errors.New("voice: active call not found")
	}
	call.setCaptureBasePath(basePath)
	return call.startConfiguredCapture()
}
