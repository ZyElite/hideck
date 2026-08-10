package voice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	simulateRegistrationTimeout = 10 * time.Second
	simulateRegistrationPoll    = 500 * time.Millisecond
)

// SimulateCall runs the recovered timed-call workflow over real IMS and media.
func (a *Agent) SimulateCall(
	ctx context.Context,
	request SimulateCallRequest,
) (*SimulateCallResult, error) {
	if a == nil || a.imsEndpoint() == nil {
		return nil, errors.New("voice: IMS provider is unavailable")
	}
	request.Callee = strings.TrimSpace(request.Callee)
	if request.Callee == "" {
		return nil, errors.New("voice: callee is empty")
	}
	if request.HoldSeconds < 0 {
		return nil, errors.New("voice: hold seconds must not be negative")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := a.waitForSimulateRegistration(ctx); err != nil {
		return &SimulateCallResult{Reason: err.Error()}, err
	}
	if a.IsBusy() {
		err := errors.New("voice: another call is active")
		return &SimulateCallResult{Reason: err.Error()}, err
	}
	startedAt := time.Now()
	inviteCtx, cancelInvite := context.WithTimeout(
		ctx, voiceInviteTimeout+outboundCancelSettle,
	)
	call, err := a.startSimulateClientInvite(inviteCtx, request.Callee)
	cancelInvite()
	if err != nil {
		return simulateFailure(startedAt, err), err
	}
	if request.OnConnected != nil {
		request.OnConnected()
	}
	return a.holdSimulatedCall(ctx, call, request.HoldSeconds, startedAt)
}

func (a *Agent) waitForSimulateRegistration(ctx context.Context) error {
	endpoint := a.imsEndpoint()
	if endpoint == nil {
		return errors.New("voice: IMS provider is unavailable")
	}
	if endpoint.IsRegistered() {
		return nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, simulateRegistrationTimeout)
	defer cancel()
	ticker := time.NewTicker(simulateRegistrationPoll)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("voice: IMS registration wait failed: %w", waitCtx.Err())
		case <-ticker.C:
			if endpoint.IsRegistered() {
				return nil
			}
		}
	}
}

func (a *Agent) holdSimulatedCall(
	ctx context.Context,
	call *Call,
	holdSeconds int,
	startedAt time.Time,
) (*SimulateCallResult, error) {
	timer := time.NewTimer(time.Duration(holdSeconds) * time.Second)
	defer timer.Stop()
	select {
	case <-call.Done:
		return &SimulateCallResult{
			Success:    false,
			DurationMs: time.Since(startedAt).Milliseconds(),
			Reason:     "中途被动终止",
		}, nil
	case <-ctx.Done():
		return a.waitSimulateCancelSettle(call, startedAt, ctx.Err())
	case mediaErr := <-call.MediaErrors():
		if mediaErr == nil {
			mediaErr = errors.New("voice: simulated media stopped")
		}
		return a.waitSimulateCancelSettle(call, startedAt, mediaErr)
	case <-timer.C:
		if err := a.closeSimulatedCall(call); err != nil {
			return simulateFailure(startedAt, err), err
		}
		return &SimulateCallResult{
			Success:    true,
			DurationMs: time.Since(startedAt).Milliseconds(),
			Reason:     "定时正常挂断",
		}, nil
	}
}

func (a *Agent) startSimulateClientInvite(ctx context.Context, callee string) (*Call, error) {
	return a.dialContext(ctx, callee, "")
}

func (a *Agent) waitSimulateCancelSettle(
	call *Call,
	startedAt time.Time,
	cause error,
) (*SimulateCallResult, error) {
	return a.failAndCloseSimulatedCall(call, startedAt, cause)
}

func (a *Agent) failAndCloseSimulatedCall(
	call *Call,
	startedAt time.Time,
	cause error,
) (*SimulateCallResult, error) {
	err := errors.Join(cause, a.closeSimulatedCall(call))
	return simulateFailure(startedAt, err), err
}

func (a *Agent) closeSimulatedCall(call *Call) error {
	if call == nil || call.IsTerminalState() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), voiceHangupTimeout)
	defer cancel()
	return a.HangupContext(ctx, call.CallID())
}

func simulateFailure(startedAt time.Time, err error) *SimulateCallResult {
	reason := ""
	if err != nil {
		reason = err.Error()
	}
	return &SimulateCallResult{
		Success: false, DurationMs: time.Since(startedAt).Milliseconds(), Reason: reason,
	}
}
