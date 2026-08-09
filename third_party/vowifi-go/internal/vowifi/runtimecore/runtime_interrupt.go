package runtimecore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
)

func runUntilInterrupted(
	ctx context.Context,
	req *RuntimeStartRequest,
	session *SessionResult,
) error {
	if req.Hooks.OnInterruptReady != nil {
		req.Hooks.OnInterruptReady(ctx)
	}
	outcome := waitRuntimeInterruption(ctx, req, session)
	stopper := req.StopSession
	if stopper == nil {
		stopper = defaultStopSession
	}
	stopper(context.Background(), session)
	if req.voiceBinding != nil {
		req.voiceBinding.Detach()
	}
	emitStopped(ctx, req, session)
	return interruptionError(ctx, outcome, session)
}

func interruptionError(
	ctx context.Context,
	outcome InterruptOutcome,
	session *SessionResult,
) error {
	switch outcome.Kind {
	case "context_cancelled":
		return ctx.Err()
	case "redirect":
		return &ErrRedirect{NewEPDG: outcome.RedirectEPDG, Delay: outcome.RetryDelay}
	case "reauth":
		return swu.ErrFreshRuntimeRequired
	case "session_down":
		if session != nil && session.Session != nil {
			if err := session.Session.TerminalError(); err != nil {
				return err
			}
		}
		return errors.New(firstNonEmpty(outcome.Reason, "runtimecore: SWu session stopped"))
	case "ims_reconnect":
		return errors.New(firstNonEmpty(outcome.Reason, "runtimecore: IMS reconnect requested"))
	default:
		return nil
	}
}

func waitRuntimeInterruption(
	ctx context.Context,
	req *RuntimeStartRequest,
	result *SessionResult,
) InterruptOutcome {
	outcomes := make(chan InterruptOutcome, 3)
	var session *swu.Session
	if result != nil {
		session = result.Session
	}
	if session != nil {
		chainSessionCallbacks(sessionCallbackConfig{
			ctx: ctx, request: req, session: session, outcomes: outcomes,
		})
	}
	var imsErrors <-chan error
	if result != nil && result.IMSService != nil {
		imsErrors = result.IMSService.RegistrationErrors()
	}
	sessionDone := sessionDoneChan(session)
	select {
	case <-ctx.Done():
		outcome := InterruptOutcome{Kind: "context_cancelled", Reason: ctx.Err().Error()}
		emitInterrupted(ctx, req, result, outcome)
		return outcome
	case err, ok := <-imsErrors:
		reason := "IMS runtime reconnect requested"
		if ok && err != nil {
			reason = err.Error()
		}
		outcome := InterruptOutcome{Kind: "ims_reconnect", Reason: reason}
		emitInterrupted(ctx, req, result, outcome)
		return outcome
	case outcome := <-outcomes:
		emitInterrupted(ctx, req, result, outcome)
		return outcome
	case <-sessionDone:
		reason := "swu_session_down"
		if err := session.TerminalError(); err != nil {
			reason = err.Error()
		}
		outcome := InterruptOutcome{Kind: "session_down", Reason: reason}
		emitInterrupted(ctx, req, result, outcome)
		return outcome
	}
}

type sessionCallbackConfig struct {
	ctx      context.Context
	request  *RuntimeStartRequest
	session  *swu.Session
	outcomes chan<- InterruptOutcome
}

func chainSessionCallbacks(config sessionCallbackConfig) {
	chainSessionDown(config)
	chainSessionReauth(config)
	chainSessionRedirect(config)
}

func chainSessionDown(config sessionCallbackConfig) {
	previous := config.session.OnSessionDown
	config.session.OnSessionDown = func() {
		if previous != nil {
			previous()
		}
		if config.request.Hooks.OnSessionDown != nil {
			config.request.Hooks.OnSessionDown(config.ctx)
		}
		sendOutcome(config.outcomes, InterruptOutcome{Kind: "session_down", Reason: "swu_session_down"})
	}
}

func chainSessionReauth(config sessionCallbackConfig) {
	previous := config.session.OnReauthNeeded
	config.session.OnReauthNeeded = func() {
		if previous != nil {
			previous()
		}
		if config.request.Hooks.OnReauthNeeded != nil {
			config.request.Hooks.OnReauthNeeded(config.ctx)
		}
		sendOutcome(config.outcomes, InterruptOutcome{Kind: "reauth", Reason: swu.ErrFreshRuntimeRequired.Error()})
	}
}

func chainSessionRedirect(config sessionCallbackConfig) {
	previous := config.session.OnRedirect
	config.session.OnRedirect = func(target string) {
		if previous != nil {
			previous(target)
		}
		if config.request.Hooks.OnRedirect != nil {
			config.request.Hooks.OnRedirect(config.ctx, target)
		}
		sendOutcome(config.outcomes, InterruptOutcome{
			Kind: "redirect", Reason: "redirect", RedirectEPDG: strings.TrimSpace(target),
			RetryDelay: int64(2 * time.Second),
		})
	}
}

func sendOutcome(ch chan<- InterruptOutcome, outcome InterruptOutcome) {
	select {
	case ch <- outcome:
	default:
	}
}

func applyRedirectOverride(req *RuntimeStartRequest, err error) {
	var redirect *ErrRedirect
	if req != nil && errors.As(err, &redirect) {
		if value := firstNonEmpty(redirect.NewEPDG, redirect.Target); value != "" {
			req.RuntimeEPDGOverride = value
		}
	}
}
