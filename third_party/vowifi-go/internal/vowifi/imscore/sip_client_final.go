package imscore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
)

func (t *sipTransport) completeClientTransaction(
	transaction *clientSIPTransaction,
	response *sipResponse,
	timers sipTransactionTimers,
) (*sipResponse, error) {
	transaction.mu.Lock()
	transaction.final = response
	transaction.mu.Unlock()
	if transaction.invite && response.StatusCode >= 300 {
		ack, err := buildNon2xxTransactionACK(transaction.parsed, response)
		if err != nil {
			return nil, t.failTransaction(transaction, err)
		}
		if err := sendClientTransaction(transaction, ack); err != nil {
			return nil, t.failTransaction(transaction, transactionTransportError(err))
		}
		transaction.mu.Lock()
		transaction.ack = ack
		transaction.mu.Unlock()
	}
	retention := clientTransactionRetention(transaction, response, timers)
	if retention <= 0 {
		t.removeTransaction(transaction)
		transaction.finish()
		return response, nil
	}
	go t.retainClientTransaction(transaction, retention)
	return response, nil
}

func clientTransactionRetention(
	transaction *clientSIPTransaction,
	response *sipResponse,
	timers sipTransactionTimers,
) time.Duration {
	if !transaction.invite {
		if transaction.reliable {
			return 0
		}
		return timers.k
	}
	if response.StatusCode < 300 {
		return timers.m
	}
	if transaction.reliable {
		return 0
	}
	return timers.d
}

func (t *sipTransport) retainClientTransaction(
	transaction *clientSIPTransaction,
	duration time.Duration,
) {
	defer t.removeTransaction(transaction)
	defer transaction.finish()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	for {
		select {
		case response := <-transaction.responses:
			t.handleRetainedClientResponse(transaction, response)
		case <-transaction.terminated:
			return
		case <-timer.C:
			return
		case <-t.closed:
			return
		}
	}
}

func (t *sipTransport) handleRetainedClientResponse(
	transaction *clientSIPTransaction,
	response *sipResponse,
) {
	if response == nil || !transaction.invite {
		return
	}
	transaction.mu.Lock()
	final := transaction.final
	ack := transaction.ack
	callback := transaction.callbacks.onFinalRetransmit
	transaction.mu.Unlock()
	if final == nil {
		return
	}
	if final.StatusCode >= 300 && response.StatusCode >= 300 {
		if err := sendClientTransaction(transaction, ack); err != nil {
			t.reportFatal(err)
			logging.WarnRate("ims-invite-ack-retransmit", "IMS INVITE ACK retransmission failed", "err", err)
		}
		return
	}
	if final.StatusCode < 300 && response.StatusCode >= 200 && response.StatusCode < 300 && callback != nil {
		if err := callback(response); err != nil {
			logging.WarnRate("ims-invite-final-retransmit", "IMS INVITE final retransmission handler failed", "err", err)
		}
	}
}

func notifyCanceledInviteFinal(
	transaction *clientSIPTransaction,
	response *sipResponse,
) error {
	if response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil
	}
	if transaction.callbacks.onFinalRetransmit == nil {
		return nil
	}
	return transaction.callbacks.onFinalRetransmit(response)
}

func buildNon2xxTransactionACK(
	invite *sip.Request,
	response *sipResponse,
) (string, error) {
	if invite == nil {
		return "", errors.New("imscore: client INVITE initial request is empty")
	}
	if response == nil || response.parsed == nil {
		return "", errors.New("imscore: client INVITE final response is empty")
	}
	ack := sip.NewRequest(sip.ACK, *invite.Recipient.Clone())
	ack.SipVersion = invite.SipVersion
	if err := appendTransactionACKHeaders(ack, invite, response.parsed); err != nil {
		return "", err
	}
	ack.SetTransport(invite.Transport())
	ack.SetSource(invite.Source())
	ack.SetDestination(invite.Destination())
	ack.SetBody(nil)
	return ack.String(), nil
}

func appendTransactionACKHeaders(ack, invite *sip.Request, response *sip.Response) error {
	if invite.Via() == nil || invite.From() == nil || invite.CallID() == nil || invite.CSeq() == nil {
		return errors.New("imscore: client INVITE transaction headers are incomplete")
	}
	if response.To() == nil {
		return errors.New("imscore: client INVITE final response has no To header")
	}
	ack.AppendHeader(invite.Via().Clone())
	appendACKRoutes(ack, invite, response)
	maxForwards := sip.MaxForwardsHeader(70)
	ack.AppendHeader(&maxForwards)
	ack.AppendHeader(sip.HeaderClone(invite.From()))
	ack.AppendHeader(sip.HeaderClone(response.To()))
	ack.AppendHeader(sip.HeaderClone(invite.CallID()))
	ack.AppendHeader(sip.HeaderClone(invite.CSeq()))
	ack.CSeq().MethodName = sip.ACK
	if invite.Contact() != nil {
		ack.AppendHeader(sip.HeaderClone(invite.Contact()))
	}
	return nil
}

func appendACKRoutes(ack, invite *sip.Request, response *sip.Response) {
	routes := invite.GetHeaders("Route")
	if len(routes) > 0 {
		for _, route := range routes {
			ack.AppendHeader(sip.HeaderClone(route))
		}
		return
	}
	recordRoutes := response.GetHeaders("Record-Route")
	for index := len(recordRoutes) - 1; index >= 0; index-- {
		ack.AppendHeader(sip.NewHeader("Route", recordRoutes[index].Value()))
	}
}

func (t *sipTransport) cancelInviteTransaction(
	transaction *clientSIPTransaction,
	timeout time.Duration,
) error {
	result, err := t.startInviteCancel(transaction, timeout)
	if err != nil {
		return err
	}
	return waitInviteCancelResult(result)
}

func (t *sipTransport) startInviteCancel(
	transaction *clientSIPTransaction,
	timeout time.Duration,
) (<-chan error, error) {
	if !transaction.beginCancel() {
		return nil, nil
	}
	cancel, err := buildClientInviteCancelRequest(transaction.parsed)
	if err != nil {
		transaction.cancelFailed()
		return nil, err
	}
	cancelTransaction, err := t.startClientTransactionWithSender(
		cancel.String(), sipTransactionCallbacks{}, transaction.send,
	)
	if err != nil {
		transaction.cancelFailed()
		return nil, fmt.Errorf("imscore: client INVITE CANCEL: %w", err)
	}
	result := make(chan error, 1)
	go t.waitInviteCancel(transaction, cancelTransaction, timeout, result)
	return result, nil
}

func buildClientInviteCancelRequest(invite *sip.Request) (*sip.Request, error) {
	cancel, err := sipkit.BuildCancelFromInvite(invite)
	if err != nil {
		return nil, fmt.Errorf("client INVITE 不能构造 CANCEL: %w", err)
	}
	return cancel, nil
}

func (t *sipTransport) waitInviteCancel(
	invite, cancel *clientSIPTransaction,
	timeout time.Duration,
	result chan<- error,
) {
	ctx, stop := context.WithTimeout(context.Background(), timeout)
	defer stop()
	response, err := t.waitClientTransaction(ctx, cancel)
	if err == nil && response.StatusCode != 200 {
		err = fmt.Errorf("imscore: client INVITE CANCEL did not receive 200 OK: %d", response.StatusCode)
	}
	if err != nil {
		invite.cancelFailed()
		result <- fmt.Errorf("imscore: client INVITE CANCEL: %w", err)
		return
	}
	invite.cancelSucceeded()
	result <- nil

}

func waitInviteCancelResult(result <-chan error) error {
	if result == nil {
		return nil
	}
	return <-result
}

func (transaction *clientSIPTransaction) beginCancel() bool {
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if transaction.canceling || transaction.cancelSent || transaction.final != nil {
		return false
	}
	transaction.canceling = true
	return true
}

func (transaction *clientSIPTransaction) cancelFailed() {
	transaction.mu.Lock()
	transaction.canceling = false
	transaction.mu.Unlock()
}

func (transaction *clientSIPTransaction) cancelSucceeded() {
	transaction.mu.Lock()
	transaction.canceling = false
	transaction.cancelSent = true
	transaction.mu.Unlock()
}
