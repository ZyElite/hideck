package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const defaultTransactionFlow = "client_transaction"

// StartTransaction creates a real sipgo client transaction and preserves the
// transaction response and error channels owned by sipgo.
func (b *Bridge) StartTransaction(
	ctx context.Context,
	flow string,
	req *sip.Request,
) (sip.ClientTransaction, error) {
	if req == nil {
		return nil, errTransactionEmpty
	}
	if b == nil || b.client == nil {
		return nil, errClientUninitialized
	}
	flow = normalizedFlow(flow, defaultTransactionFlow)
	ctx = b.transactionContext(ctx)
	startedAt := time.Now()
	transaction, err := b.client.TransactionRequest(ctx, req)
	if err == nil {
		logging.RunDebug("voice client transaction started", "device", b.deviceID,
			"flow", flow, "line", req.StartLine(), "elapsed_ms", time.Since(startedAt).Milliseconds())
		return transaction, nil
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, err
	}
	logging.WarnRate("voice_client_transaction_timeout:"+b.deviceID+":"+flow, 10*time.Second,
		"voice client transaction timeout", "device", b.deviceID, "flow", flow,
		"line", req.StartLine(), "elapsed_ms", time.Since(startedAt).Milliseconds())
	return nil, fmt.Errorf("%w: flow=%s line=%s", ErrVoiceClientTransactionTimeout, flow, req.StartLine())
}

func (b *Bridge) transactionContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctx != nil {
		return b.ctx
	}
	return context.Background()
}
