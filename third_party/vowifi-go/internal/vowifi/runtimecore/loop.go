package runtimecore

import (
	"context"
	"errors"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
)

func RunLoop(
	ctx context.Context,
	reconnectDelay func(int) int64,
	before func(),
	onRetry func(int, int64),
	run func(context.Context) error,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if run == nil {
		return errors.New("runtimecore: nil run function")
	}
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if before != nil {
			before()
		}
		err := run(ctx)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errors.Is(err, context.Canceled) {
			return err
		}
		delay, nextAttempt := retryDecision(err, attempt, reconnectDelay)
		attempt = nextAttempt
		if delay <= 0 {
			continue
		}
		if onRetry != nil {
			onRetry(attempt, delay)
		}
		if err := waitRetry(ctx, time.Duration(delay)); err != nil {
			return err
		}
	}
}

func retryDecision(err error, attempt int, delayFn func(int) int64) (int64, int) {
	if err == nil || err == swu.ErrFreshRuntimeRequired {
		return 0, 0
	}
	redirect, ok := err.(*ErrRedirect)
	if ok {
		return redirect.Delay, 0
	}
	if delayFn == nil {
		return 0, attempt + 1
	}
	return delayFn(attempt), attempt + 1
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
