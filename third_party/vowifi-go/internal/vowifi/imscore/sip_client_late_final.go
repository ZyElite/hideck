package imscore

import (
	"context"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

func shouldRetainClientTransaction(ctx context.Context, transaction *clientSIPTransaction) bool {
	if ctx == nil || transaction == nil || transaction.invite {
		return false
	}
	callbacks := transaction.callbacks
	if callbacks.onLateFinal == nil || callbacks.retainFinalAfterContext == nil {
		return false
	}
	return callbacks.retainFinalAfterContext(context.Cause(ctx))
}

func (t *sipTransport) waitLateClientTransaction(
	transaction *clientSIPTransaction,
	timers sipTransactionTimers,
) {
	retention := transaction.callbacks.lateFinalRetention
	if retention <= 0 || retention > timers.bf {
		retention = timers.bf
	}
	timeout := time.NewTimer(retention)
	defer timeout.Stop()

	for {
		select {
		case response := <-transaction.responses:
			if t.handleLateClientResponse(transaction, response, timers) {
				return
			}
		case err := <-transaction.terminated:
			t.failTransaction(transaction, err)
			return
		case <-timeout.C:
			t.abandonClientTransaction(transaction)
			return
		case <-t.closed:
			t.abandonClientTransaction(transaction)
			return
		}
	}
}

func (t *sipTransport) handleLateClientResponse(
	transaction *clientSIPTransaction,
	response *sipResponse,
	timers sipTransactionTimers,
) bool {
	if response == nil {
		return false
	}
	if response.StatusCode < 200 {
		transaction.markProceeding()
		if err := notifyProvisional(transaction, response); err != nil {
			t.failTransaction(transaction, err)
			return true
		}
		return false
	}
	final, err := t.completeClientTransaction(transaction, response, timers)
	if err != nil {
		return true
	}
	if err := transaction.callbacks.onLateFinal(final); err != nil {
		logging.WarnRate("ims-late-sip-final-handler", "IMS late SIP final handler failed",
			"method", transaction.key.Method, "status", response.StatusCode, "err", err)
	}
	return true
}

func (t *sipTransport) abandonClientTransaction(transaction *clientSIPTransaction) {
	t.removeTransaction(transaction)
	transaction.finish()
}
