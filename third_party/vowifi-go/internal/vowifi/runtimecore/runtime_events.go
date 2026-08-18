package runtimecore

import (
	"context"
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

func snapshotFromSessionResult(result *SessionResult) Snapshot {
	if result == nil {
		return Snapshot{}
	}
	return Snapshot{
		Established: result.Snapshot.Established,
		InnerIP:     strings.TrimSpace(result.LocalAddr),
		TUNName:     strings.TrimSpace(result.Snapshot.TUNName),
	}
}

func emitAllRuntimeEvents(
	ctx context.Context,
	req *RuntimeStartRequest,
	event RuntimeEvent[*SessionResult],
) {
	if req.Observer != nil {
		req.Observer.OnRuntimeEvent(ctx, event)
	}
	if req.Hooks.Events != nil {
		req.Hooks.Events.OnRuntimeEvent(ctx, event)
	}
}

func emitInterrupted(ctx context.Context, req *RuntimeStartRequest, session *SessionResult, outcome InterruptOutcome) {
	err := interruptionError(ctx, outcome, session)
	deviceID, traceID := "", ""
	if req != nil {
		deviceID = req.DeviceID
		traceID = req.TraceID
	}
	logging.Info("VoWiFi runtime interrupted",
		"device", deviceID, "trace_id", traceID,
		"kind", outcome.Kind, "reason", outcome.Reason,
		"redirect_epdg", outcome.RedirectEPDG, "retry_delay", outcome.RetryDelay)
	emitAllRuntimeEvents(ctx, req, RuntimeEvent[*SessionResult]{
		Kind: "interrupted", Handle: session, DeviceID: req.DeviceID, TraceID: req.TraceID,
		Snapshot: snapshotFromSessionResult(session), Reason: outcome.Reason,
		RetryDelay: outcome.RetryDelay, RedirectEPDG: outcome.RedirectEPDG,
	})
	if req.Hooks.OnInterrupted != nil {
		req.Hooks.OnInterrupted(ctx, err)
	}
}

func emitStopped(ctx context.Context, req *RuntimeStartRequest, session *SessionResult) {
	emitAllRuntimeEvents(ctx, req, RuntimeEvent[*SessionResult]{
		Kind: "stopped", Handle: session, DeviceID: req.DeviceID,
		TraceID: req.TraceID, Snapshot: snapshotFromSessionResult(session),
	})
	if req.Hooks.OnStopped != nil {
		req.Hooks.OnStopped(ctx)
	}
}

func emitRetry(ctx context.Context, req *RuntimeStartRequest, attempt int, delay int64) {
	emitAllRuntimeEvents(ctx, req, RuntimeEvent[*SessionResult]{
		Kind: "retry", DeviceID: req.DeviceID, TraceID: req.TraceID,
		Attempt: attempt, RetryDelay: delay,
	})
	if req.Hooks.OnRetryDelay != nil {
		req.Hooks.OnRetryDelay(ctx, attempt, delay)
	}
}

func emitRuntimeError(ctx context.Context, req *RuntimeStartRequest, err error) {
	emitAllRuntimeEvents(ctx, req, RuntimeEvent[*SessionResult]{
		Kind: "error", DeviceID: req.DeviceID, TraceID: req.TraceID,
		Message: err.Error(), Reason: err.Error(),
	})
	if req.Hooks.OnError != nil {
		req.Hooks.OnError(ctx, err)
	}
}

func (outcome InterruptOutcome) Error() string {
	if outcome.Reason != "" {
		return outcome.Reason
	}
	return fmt.Sprintf("runtime interrupted: %s", outcome.Kind)
}
