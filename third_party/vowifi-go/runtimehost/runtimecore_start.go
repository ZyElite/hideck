package runtimehost

import (
	"context"
	"errors"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
	"github.com/iniwex5/vowifi-go/internal/vowifi/runtimecore"
	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

type coreRunner func(context.Context, runtimecore.RuntimeStartRequest) (StartResult, error)
type reconnectDelay func(int) int64

func (req StartRequest) coreRequest() runtimecore.RuntimeStartRequest {
	request := runtimecore.RuntimeStartRequest{
		DeviceID: strings.TrimSpace(req.DeviceID), TraceID: strings.TrimSpace(req.TraceID),
		Profile: profile.Profile{
			IMSI: req.Profile.IMSI, MCC: req.Profile.MCC, MNC: req.Profile.MNC,
			IMEI: req.Profile.IMEI, UserAgent: req.Profile.UserAgent,
			SMSC: req.Profile.SMSC, IMSDomain: req.Profile.IMSDomain,
		},
		Prepared: preparedSessionPtrToInternal(req.Prepared),
		IMSIdentity: profile.IMSIdentityResult{
			RequestedSource:  req.IMSIdentity.RequestedSource,
			ActualSource:     req.IMSIdentity.ActualSource,
			AKAAppPreference: req.IMSIdentity.AKAAppPreference,
			Applied:          req.IMSIdentity.Applied, IMPI: req.IMSIdentity.IMPI,
			IMPU: req.IMSIdentity.IMPU, Domain: req.IMSIdentity.Domain,
		},
		SIM: req.SIM.runtimeSIMAdapter(),
		Dataplane: runtimecore.RuntimeDataplanePolicy{
			Mode: req.Dataplane.Mode, TUNName: req.Dataplane.TUNName,
		},
		Proxy: runtimeCoreProxy(req.Proxy), DNSServer: req.DNSServer,
		DeliveryStore: runtimeCoreDeliveryStore(req.DeliveryStore),
		Dispatch:      runtimeCoreDispatcher(req.Dispatch), ShouldRun: req.ShouldRun,
	}
	if req.BeforeStart != nil {
		hook := req.BeforeStart
		request.BeforeSessionStart = func(ctx context.Context, config runtimecore.SessionConfig) error {
			return apiErrorToInternal(hook(ctx, sessionConfigFromInternal(config)))
		}
	}
	if req.Access != nil {
		request.Access = accessAdapter{host: req.Access}
	}
	if req.VoiceGateway != nil {
		request.Options.Voice = voiceRuntimeLifecycle{gateway: req.VoiceGateway}
	}
	return request
}

func startInstance(
	ctx context.Context,
	request runtimecore.RuntimeStartRequest,
	runner coreRunner,
	delay reconnectDelay,
) (*Instance, error) {
	instance, ready, done := launchRuntimeCore(ctx, request, runner, delay)
	select {
	case <-ctx.Done():
		_ = instance.Stop(context.Background())
		return nil, ctx.Err()
	case err := <-done:
		_ = instance.Stop(context.Background())
		if err == nil {
			err = errors.New("runtimehost: runtime core stopped before IPsec became ready")
		}
		return nil, err
	case <-ready:
		return instance, nil
	}
}

func startInstanceAsync(
	ctx context.Context,
	request runtimecore.RuntimeStartRequest,
	runner coreRunner,
	delay reconnectDelay,
) (*Instance, error) {
	instance, _, _ := launchRuntimeCore(ctx, request, runner, delay)
	return instance, nil
}

func launchRuntimeCore(
	ctx context.Context,
	request runtimecore.RuntimeStartRequest,
	runner coreRunner,
	delay reconnectDelay,
) (*Instance, <-chan struct{}, <-chan error) {
	runCtx, cancel := context.WithCancel(ctx)
	instance := &Instance{cancel: cancel}
	instance.setState(coreInitialState(request))
	ready := make(chan struct{})
	observer := &instanceObserver{
		inst: instance, deviceID: request.DeviceID, ready: ready,
	}
	request.Observer = observer
	request.Reconnect = true
	request.ReconnectDelay = delay
	chainSMSReadyHook(&request, observer)
	if runner == nil {
		runner = defaultCoreRunner
	}
	done := make(chan error, 1)
	go func() {
		_, err := runner(runCtx, request)
		done <- apiErrorToInternal(err)
	}()
	return instance, ready, done
}

func coreInitialState(request runtimecore.RuntimeStartRequest) State {
	return State{
		Phase: "starting", DeviceID: request.DeviceID,
		DataplaneMode: request.Dataplane.Mode, SIMReady: request.SIM != nil,
		AccessReady: request.Access != nil, LastEvent: "starting",
		SessionState: "starting",
	}
}

func chainSMSReadyHook(
	request *runtimecore.RuntimeStartRequest,
	observer *instanceObserver,
) {
	previous := request.Hooks.OnSMSReady
	request.Hooks.OnSMSReady = func(ctx context.Context) {
		if previous != nil {
			previous(ctx)
		}
		observer.OnRuntimeEvent(ctx, runtimecore.RuntimeEvent[*runtimecore.SessionResult]{
			Kind: "sms_ready", DeviceID: request.DeviceID, TraceID: request.TraceID,
		})
	}
}

func defaultCoreRunner(
	ctx context.Context,
	request runtimecore.RuntimeStartRequest,
) (StartResult, error) {
	result, err := (runtimecore.Runtime{}).Start(ctx, request)
	return startResultFromInternal(result), err
}

func startResultFromInternal(result runtimecore.RuntimeStartResult) StartResult {
	return StartResult{TraceID: result.TraceID}
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

type voiceRuntimeLifecycle struct {
	gateway *voicehost.Gateway
}

func (adapter voiceRuntimeLifecycle) AttachDevice(
	deviceID string,
	endpoint imsendpoint.Endpoint,
) error {
	return adapter.gateway.AttachDeviceCurrent(deviceID, endpoint)
}

func (adapter voiceRuntimeLifecycle) DetachDevice(deviceID string) {
	adapter.gateway.DetachDeviceCurrent(deviceID)
}
