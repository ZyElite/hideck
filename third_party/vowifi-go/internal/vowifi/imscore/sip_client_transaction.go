package imscore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const sipTransactionResponseBuffer = 64

type sipTransactionTimers struct {
	t1 time.Duration
	t2 time.Duration
	bf time.Duration
	d  time.Duration
	k  time.Duration
	m  time.Duration
}

func defaultSIPTransactionTimers() sipTransactionTimers {
	return sipTransactionTimers{
		t1: 500 * time.Millisecond,
		t2: 4 * time.Second,
		bf: 32 * time.Second,
		d:  32 * time.Second,
		k:  5 * time.Second,
		m:  32 * time.Second,
	}
}

type clientSIPTransaction struct {
	key       sipTransactionKey
	request   string
	parsed    *sip.Request
	send      func(string) error
	invite    bool
	reliable  bool
	callbacks sipTransactionCallbacks

	responses  chan *sipResponse
	terminated chan error
	done       chan struct{}
	doneOnce   sync.Once

	mu         sync.Mutex
	proceeding bool
	final      *sipResponse
	ack        string
	canceling  bool
	cancelSent bool
}

func newClientSIPTransaction(
	key sipTransactionKey,
	request string,
	parsed *sip.Request,
	send func(string) error,
	callbacks sipTransactionCallbacks,
) *clientSIPTransaction {
	return &clientSIPTransaction{
		key: key, request: request, parsed: parsed, send: send,
		invite: parsed.IsInvite(), reliable: sip.IsReliable(parsed.Transport()),
		callbacks: callbacks, responses: make(chan *sipResponse, sipTransactionResponseBuffer),
		terminated: make(chan error, 1), done: make(chan struct{}),
	}
}

func (transaction *clientSIPTransaction) deliver(response *sipResponse) bool {
	select {
	case <-transaction.done:
		return false
	case transaction.responses <- response:
		return true
	}
}

func (transaction *clientSIPTransaction) terminate(err error) {
	select {
	case <-transaction.done:
	case transaction.terminated <- err:
	default:
	}
}

func (transaction *clientSIPTransaction) finish() {
	transaction.doneOnce.Do(func() { close(transaction.done) })
}

func (transaction *clientSIPTransaction) markProceeding() {
	transaction.mu.Lock()
	transaction.proceeding = true
	transaction.mu.Unlock()
}

func (transaction *clientSIPTransaction) isProceeding() bool {
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	return transaction.proceeding
}

func (t *sipTransport) transactionTimers() sipTransactionTimers {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.timers
}

func (t *sipTransport) waitClientTransaction(
	ctx context.Context,
	transaction *clientSIPTransaction,
) (*sipResponse, error) {
	timers := t.transactionTimers()
	timeout := time.NewTimer(timers.bf)
	defer timeout.Stop()
	retransmit, retransmitAt := newRetransmitTimer(transaction, timers.t1)
	defer stopTransactionTimer(retransmit)

	ctxDone := ctx.Done()
	var cancelCause error
	var cancelResult <-chan error
	for {
		select {
		case response := <-transaction.responses:
			if response == nil {
				continue
			}
			if response.StatusCode < 200 {
				transaction.markProceeding()
				stopTransactionTimer(retransmit)
				retransmit = nil
				if err := notifyProvisional(transaction, response); err != nil {
					return nil, t.failTransaction(transaction, err)
				}
				if cancelCause != nil && cancelResult == nil {
					var err error
					cancelResult, err = t.startInviteCancel(transaction, timers.bf)
					if err != nil {
						return nil, t.failTransaction(transaction, errors.Join(cancelCause, err))
					}
				}
				continue
			}
			if cancelCause != nil && !transaction.isProceeding() {
				_, _ = t.completeClientTransaction(transaction, response, timers)
				return nil, fmt.Errorf("imscore: final INVITE response arrived before CANCEL: %w", cancelCause)
			}
			result, err := t.completeClientTransaction(transaction, response, timers)
			if err != nil {
				return nil, err
			}
			if cancelCause != nil {
				if err := notifyCanceledInviteFinal(transaction, response); err != nil {
					return nil, errors.Join(cancelCause, err)
				}
				if err := waitInviteCancelResult(cancelResult); err != nil {
					return nil, errors.Join(cancelCause, err)
				}
				return nil, cancelCause
			}
			return result, nil
		case <-ctxDone:
			if !transaction.invite {
				return nil, t.failTransaction(transaction, ctx.Err())
			}
			cancelCause = ctx.Err()
			ctxDone = nil
			if transaction.isProceeding() {
				var err error
				cancelResult, err = t.startInviteCancel(transaction, timers.bf)
				if err != nil {
					return nil, t.failTransaction(transaction, errors.Join(cancelCause, err))
				}
			}
		case err := <-cancelResult:
			cancelResult = nil
			if err != nil {
				return nil, t.failTransaction(transaction, errors.Join(cancelCause, err))
			}
		case <-transactionTimerChannel(retransmit):
			if err := sendClientTransaction(transaction, transaction.request); err != nil {
				return nil, t.failTransaction(transaction, transactionTransportError(err))
			}
			retransmitAt = nextRetransmitInterval(transaction.invite, retransmitAt, timers.t2)
			retransmit.Reset(retransmitAt)
		case err := <-transaction.terminated:
			return nil, t.failTransaction(transaction, err)
		case <-timeout.C:
			err := fmt.Errorf("imscore: SIP %s transaction timed out: %w", transaction.key.Method, sip.ErrTransactionTimeout)
			if cancelCause != nil {
				err = errors.Join(cancelCause, err)
			}
			return nil, t.failTransaction(transaction, err)
		case <-t.closed:
			return nil, t.failTransaction(transaction, sip.ErrTransactionTerminated)
		}
	}
}

func sendClientTransaction(transaction *clientSIPTransaction, request string) error {
	if transaction == nil || transaction.send == nil {
		return errors.New("imscore: SIP transport sender is not configured")
	}
	return transaction.send(request)
}

func notifyProvisional(transaction *clientSIPTransaction, response *sipResponse) error {
	if transaction.callbacks.onProvisional == nil {
		return nil
	}
	return transaction.callbacks.onProvisional(response)
}

func newRetransmitTimer(transaction *clientSIPTransaction, interval time.Duration) (*time.Timer, time.Duration) {
	if transaction.reliable {
		return nil, interval
	}
	return time.NewTimer(interval), interval
}

func nextRetransmitInterval(invite bool, current, maximum time.Duration) time.Duration {
	next := current * 2
	if !invite && next > maximum {
		return maximum
	}
	return next
}

func transactionTimerChannel(timer *time.Timer) <-chan time.Time {
	if timer == nil {
		return nil
	}
	return timer.C
}

func stopTransactionTimer(timer *time.Timer) {
	if timer != nil {
		timer.Stop()
	}
}

func transactionTransportError(err error) error {
	return fmt.Errorf("imscore: SIP transaction transport: %w: %w", err, sip.ErrTransactionTransport)
}

func (t *sipTransport) failTransaction(transaction *clientSIPTransaction, err error) error {
	t.removeTransaction(transaction)
	transaction.finish()
	t.reportFatal(err)
	return err
}

func (t *sipTransport) startDetachedTransaction(request string) error {
	transaction, err := t.startClientTransaction(request, sipTransactionCallbacks{})
	if err != nil {
		return err
	}
	go func() {
		if _, waitErr := t.waitClientTransaction(context.Background(), transaction); waitErr != nil &&
			!errors.Is(waitErr, sip.ErrTransactionTerminated) {
			logging.WarnRate("ims-detached-sip-transaction", "IMS detached SIP transaction failed",
				"method", transaction.key.Method, "err", waitErr)
		}
	}()
	return nil
}

func (t *sipTransport) terminateClientTransactions(err error) {
	if err == nil {
		err = sip.ErrTransactionTransport
	}
	t.mu.Lock()
	transactions := make([]*clientSIPTransaction, 0, len(t.waiters))
	for _, transaction := range t.waiters {
		transactions = append(transactions, transaction)
	}
	t.mu.Unlock()
	for _, transaction := range transactions {
		transaction.terminate(err)
	}
}
