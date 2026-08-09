package runtimehost

import (
	"context"
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/internal/vowifi/runtimecore"
)

func startRuntimeCore(ctx context.Context, req StartRequest) (*Instance, error) {
	inst := &Instance{}
	inst.setState(initialState(req))
	runCtx, cancel := context.WithCancel(ctx)
	coreRequest, err := runtimeCoreStartRequest(req, inst)
	if err != nil {
		cancel()
		inst.setStartFailure(err)
		return inst, err
	}
	result, err := (runtimecore.Runtime{}).Start(runCtx, coreRequest)
	if err != nil {
		cancel()
		startErr := fmt.Errorf("runtimehost: runtime core start failed: %w", err)
		inst.setStartFailure(startErr)
		return inst, startErr
	}
	installation := runtimeCoreInstallation{
		ctx: runCtx, request: req, instance: inst, cancel: cancel,
	}
	if err := installation.install(result); err != nil {
		cancel()
		runtimecore.StopSession(context.Background(), result.Session)
		return failIMSStart(inst, err)
	}
	go stopRuntimeOnContext(runCtx, inst)
	return inst, nil
}

func runtimeCoreStartRequest(
	req StartRequest,
	inst *Instance,
) (runtimecore.RuntimeStartRequest, error) {
	prepared, err := runtimecore.AdaptCompatibilityPreparedSession(
		preparedForRuntimeCore(req.Prepared),
	)
	if err != nil {
		return runtimecore.RuntimeStartRequest{}, err
	}
	return runtimecore.RuntimeStartRequest{
		DeviceID: req.DeviceID,
		TraceID:  req.TraceID,
		Prepared: &prepared,
		SIM:      runtimeCoreSIMAdapter{aka: req.SIM.AKAProvider()},
		Dataplane: runtimecore.RuntimeDataplanePolicy{
			Mode: req.Dataplane.Mode,
		},
		Proxy:         runtimeCoreProxy(req.Proxy),
		DeliveryStore: runtimeCoreDeliveryStore(req.DeliveryStore),
		Dispatch:      runtimeCoreDispatcher(req.Dispatch),
		Hooks: runtimecore.RuntimeHostHooks{
			OnConnecting: func(context.Context) { inst.updateTunnelState("connecting") },
		},
		BeforeSessionStart: runtimeCoreBeforeStart(req.BeforeStart),
	}, nil
}

func runtimeCoreBeforeStart(
	hook func(context.Context, SessionConfig) error,
) func(context.Context, runtimecore.SessionConfig) error {
	if hook == nil {
		return nil
	}
	return func(ctx context.Context, cfg runtimecore.SessionConfig) error {
		return hook(ctx, SessionConfig{DataplaneMode: cfg.DataplaneMode})
	}
}

func runtimeCoreProxy(proxy *ProxyConfig) *runtimecore.ProxyConfig {
	if proxy == nil {
		return nil
	}
	return &runtimecore.ProxyConfig{
		ID: proxy.ID, Addr: proxy.Addr, Username: proxy.Username,
		Password: proxy.Password, Enabled: proxy.Enabled,
	}
}

type runtimeCoreInstallation struct {
	ctx      context.Context
	request  StartRequest
	instance *Instance
	cancel   context.CancelFunc
}

func (installation runtimeCoreInstallation) install(result runtimecore.RuntimeStartResult) error {
	if result.Session == nil || result.Session.Session == nil || result.Session.EPDGMgr == nil {
		return errors.New("runtimehost: runtime core returned no active SWu session")
	}
	if result.Session.IMSService == nil {
		return errors.New("runtimehost: runtime core returned no IMS service")
	}
	tunnel := &swuTunnelAdapter{
		Session: result.Session.Session, manager: result.Session.EPDGMgr,
		deviceID: installation.request.DeviceID,
	}
	service := newServiceAdapter(result.Session.IMSService)
	installation.instance.attachTunnel(tunnel, installation.cancel)
	installation.instance.setService(service)
	wireSMSReadiness(installation.instance, service)
	installation.instance.markTunnelReadyForIMS()
	installation.instance.markIMSRegistered()
	syncSMSReadiness(installation.instance, service)
	if err := attachVoiceAgent(installation.request, installation.instance, service); err != nil {
		return fmt.Errorf("runtimehost: attach voice agent: %w", err)
	}
	go monitorTunnelFailure(installation.ctx, installation.instance, tunnel)
	go monitorRegistrationFailures(installation.ctx, installation.instance, service)
	return nil
}
