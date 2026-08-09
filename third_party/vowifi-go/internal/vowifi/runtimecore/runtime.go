package runtimecore

import (
	"context"
	"errors"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
)

const defaultRuntimeTraceID = "runtime"

func (Runtime) Start(
	ctx context.Context,
	req RuntimeStartRequest,
) (RuntimeStartResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.TraceID = firstNonEmpty(req.TraceID, defaultRuntimeTraceID)
	if req.DeviceID == "" {
		return RuntimeStartResult{TraceID: req.TraceID}, errors.New("runtimecore: device ID is required")
	}
	if req.Options.Voice != nil && req.voiceBinding == nil {
		req.voiceBinding = &voiceLifecycleBinding{deviceID: req.DeviceID, voice: req.Options.Voice}
	}
	if !req.Reconnect || req.DryRun {
		return (Runtime{}).startOnce(ctx, &req)
	}
	if req.voiceBinding != nil {
		defer req.voiceBinding.Stop()
	}
	return runReconnectLoop(ctx, &req)
}

func runReconnectLoop(
	ctx context.Context,
	req *RuntimeStartRequest,
) (RuntimeStartResult, error) {
	result := RuntimeStartResult{TraceID: req.TraceID}
	err := RunLoop(
		ctx,
		req.ReconnectDelay,
		func() {},
		func(attempt int, delay int64) { emitRetry(ctx, req, attempt, delay) },
		func(runCtx context.Context) error {
			started, err := (Runtime{}).startOnce(runCtx, req)
			if err != nil {
				applyRedirectOverride(req, err)
				return err
			}
			result = started
			if started.Session == nil {
				return nil
			}
			return runUntilInterrupted(runCtx, req, started.Session)
		},
	)
	return result, err
}

func (Runtime) startOnce(
	ctx context.Context,
	req *RuntimeStartRequest,
) (RuntimeStartResult, error) {
	result := RuntimeStartResult{TraceID: req.TraceID}
	if req.ShouldRun != nil && !req.ShouldRun() {
		return result, context.Canceled
	}
	if req.SIM == nil {
		err := errors.New("runtimecore: SIM adapter is required")
		emitRuntimeError(ctx, req, err)
		return result, err
	}
	if req.DryRun {
		return result, nil
	}
	prepared, err := PrepareSessionStart(ctx, *req)
	if err != nil {
		emitRuntimeError(ctx, req, err)
		return result, err
	}
	if req.Hooks.OnPrepared != nil {
		req.Hooks.OnPrepared(ctx, prepared)
	}
	emitAllRuntimeEvents(ctx, req, RuntimeEvent[*SessionResult]{
		Kind: "prepared", DeviceID: req.DeviceID, TraceID: req.TraceID,
		Identity: prepared.IMSIdentity,
	})
	if req.Hooks.OnConnecting != nil {
		req.Hooks.OnConnecting(ctx)
	}
	emitAllRuntimeEvents(ctx, req, RuntimeEvent[*SessionResult]{
		Kind: "connecting", DeviceID: req.DeviceID, TraceID: req.TraceID,
		Identity: prepared.IMSIdentity,
	})
	notifier := newIMSRegisteredNotifier(ctx, req, prepared.IMSIdentity)
	config := sessionConfigFromRequest(ctx, req, prepared, notifier)
	if req.BeforeSessionStart != nil {
		if err := req.BeforeSessionStart(ctx, config); err != nil {
			emitRuntimeError(ctx, req, err)
			return result, err
		}
	}
	starter := req.SessionStarter
	if starter == nil {
		starter = defaultSessionStarter
	}
	session, err := starter(ctx, config)
	if err != nil {
		emitRuntimeError(ctx, req, err)
		return result, err
	}
	if session == nil {
		err := errors.New("runtimecore: session starter returned nil session")
		emitRuntimeError(ctx, req, err)
		return result, err
	}
	notifier.SetSession(session)
	result.Session = session
	snapshot := snapshotFromSessionResult(session)
	if snapshot.Established {
		emitEstablished(ctx, req, result, prepared.IMSIdentity, snapshot)
	}
	return result, nil
}

func sessionConfigFromRequest(
	ctx context.Context,
	req *RuntimeStartRequest,
	prepared profile.PreparedSession,
	notifier *imsRegisteredNotifier,
) SessionConfig {
	return SessionConfig{
		Ctx: ctx, DeviceID: req.DeviceID, TraceID: req.TraceID, Prepared: prepared,
		SIM: req.SIM, DataplaneMode: firstNonEmpty(req.Dataplane.Mode, swu.DataplaneModeUserspace),
		TUNName: req.Dataplane.TUNName,
		Proxy:   req.Proxy, DNSServer: req.DNSServer, DeliveryStore: req.DeliveryStore,
		Dispatch: req.Dispatch, OnIMSRegistered: notifier.OnIMSRegistered,
		OnSMSReady: func() {
			if req.Hooks.OnSMSReady != nil {
				req.Hooks.OnSMSReady(ctx)
			}
		},
		OnProgress: req.OnProgress,
	}
}

func emitEstablished(
	ctx context.Context,
	req *RuntimeStartRequest,
	result RuntimeStartResult,
	identity profile.IMSIdentityResult,
	snapshot Snapshot,
) {
	event := RuntimeEvent[*SessionResult]{
		Kind: "established", Handle: result.Session, DeviceID: req.DeviceID,
		TraceID: req.TraceID, Identity: identity, Snapshot: snapshot,
	}
	if result.Session != nil {
		event.Service = result.Session.IMSService
	}
	emitAllRuntimeEvents(ctx, req, event)
	if req.Hooks.OnEstablished != nil {
		req.Hooks.OnEstablished(ctx, result)
	}
}
