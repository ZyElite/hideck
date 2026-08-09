package imscore

import "context"

const suppressTGSuccessContextKey = 0

func shouldSendTGSuccess(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	suppressed, _ := ctx.Value(suppressTGSuccessContextKey).(bool)
	return !suppressed
}

func withSuppressTGSuccess(ctx context.Context, suppressed bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, suppressTGSuccessContextKey, suppressed)
}
