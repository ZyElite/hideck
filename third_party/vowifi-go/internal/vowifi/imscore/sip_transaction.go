package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/emiago/sipgo/sip"
)

type sipTransactionKey struct {
	CallID string
	CSeq   int
	Method string
	Branch string
}

type sipTransactionCallbacks struct {
	onProvisional     func(*sipResponse) error
	onFinalRetransmit func(*sipResponse) error
}

func (t *sipTransport) RoundTrip(ctx context.Context, request string) (*sipResponse, error) {
	return t.roundTripWithCallbacks(ctx, request, sipTransactionCallbacks{})
}

func (t *sipTransport) roundTripWithProvisional(
	ctx context.Context,
	request string,
	onProvisional func(*sipResponse) error,
) (*sipResponse, error) {
	return t.roundTripWithCallbacks(ctx, request, sipTransactionCallbacks{
		onProvisional: onProvisional,
	})
}

func (t *sipTransport) roundTripWithCallbacks(
	ctx context.Context,
	request string,
	callbacks sipTransactionCallbacks,
) (*sipResponse, error) {
	return t.roundTripWithSenderAndCallbacks(ctx, request, nil, callbacks)
}

func (t *sipTransport) roundTripWithSender(
	ctx context.Context,
	request string,
	sender func(string) error,
) (*sipResponse, error) {
	if sender == nil {
		return nil, errors.New("imscore: nil SIP transaction sender")
	}
	return t.roundTripWithSenderAndCallbacks(ctx, request, sender, sipTransactionCallbacks{})
}

func (t *sipTransport) roundTripWithSenderAndCallbacks(
	ctx context.Context,
	request string,
	sender func(string) error,
	callbacks sipTransactionCallbacks,
) (*sipResponse, error) {
	if t == nil {
		return nil, errors.New("imscore: nil SIP transport")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	transaction, err := t.startClientTransactionWithSender(request, callbacks, sender)
	if err != nil {
		return nil, err
	}
	return t.waitClientTransaction(ctx, transaction)
}

func (t *sipTransport) startClientTransaction(
	request string,
	callbacks sipTransactionCallbacks,
) (*clientSIPTransaction, error) {
	return t.startClientTransactionWithSender(request, callbacks, nil)
}

func (t *sipTransport) startClientTransactionWithSender(
	request string,
	callbacks sipTransactionCallbacks,
	sender func(string) error,
) (*clientSIPTransaction, error) {
	message, err := parseSIPMessage(request)
	if err != nil {
		return nil, fmt.Errorf("imscore: parse SIP transaction request: %w", err)
	}
	parsed, ok := message.(*sip.Request)
	if !ok {
		return nil, errors.New("imscore: SIP transaction input is not a request")
	}
	if parsed.IsAck() {
		return nil, errors.New("imscore: ACK does not create a client transaction")
	}
	key, err := transactionKeyFromRequest(request)
	if err != nil {
		return nil, err
	}
	if sender == nil {
		t.mu.Lock()
		sender = t.sendFn
		t.mu.Unlock()
	}
	transaction := newClientSIPTransaction(key, request, parsed, sender, callbacks)
	if err := t.addTransaction(transaction); err != nil {
		return nil, err
	}
	if err := sendClientTransaction(transaction, request); err != nil {
		t.removeTransaction(transaction)
		transaction.finish()
		t.reportFatal(err)
		return nil, fmt.Errorf("imscore: SIP transaction send: %w: %w", err, sip.ErrTransactionTransport)
	}
	return transaction, nil
}

func (t *sipTransport) addTransaction(transaction *clientSIPTransaction) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case <-t.closed:
		return errors.New("imscore: SIP transport closed")
	default:
	}
	if _, exists := t.waiters[transaction.key]; exists {
		return fmt.Errorf("imscore: duplicate SIP transaction %+v", transaction.key)
	}
	t.waiters[transaction.key] = transaction
	return nil
}

func (t *sipTransport) removeTransaction(transaction *clientSIPTransaction) {
	if transaction == nil {
		return
	}
	t.mu.Lock()
	if t.waiters[transaction.key] == transaction {
		delete(t.waiters, transaction.key)
	}
	t.mu.Unlock()
}

func transactionKeyFromRequest(request string) (sipTransactionKey, error) {
	method := strings.ToUpper(sipRequestMethod(request))
	if method == "" {
		return sipTransactionKey{}, errors.New("imscore: invalid SIP transaction request")
	}
	return transactionKeyFromHeaders(
		rawSIPHeaderValue(request, "Call-ID"), rawSIPHeaderValue(request, "CSeq"),
		rawSIPHeaderValue(request, "Via"), method,
	)
}

func transactionKeyFromResponse(response *sipResponse) (sipTransactionKey, error) {
	if response == nil {
		return sipTransactionKey{}, errors.New("imscore: nil SIP response")
	}
	return transactionKeyFromHeaders(response.CallID, response.CSeq, response.Header("Via"), "")
}

func transactionKeyFromHeaders(callID, cseqValue, via, requestMethod string) (sipTransactionKey, error) {
	if strings.TrimSpace(callID) == "" {
		return sipTransactionKey{}, errors.New("imscore: SIP transaction has no Call-ID")
	}
	cseq, method, err := parseSIPCSeq(cseqValue)
	if err != nil {
		return sipTransactionKey{}, fmt.Errorf("imscore: invalid SIP transaction CSeq: %w", err)
	}
	if requestMethod != "" && !strings.EqualFold(requestMethod, method) {
		return sipTransactionKey{}, fmt.Errorf("imscore: SIP request method %s disagrees with CSeq %s", requestMethod, method)
	}
	branch, err := parseTopViaBranch(via)
	if err != nil {
		return sipTransactionKey{}, fmt.Errorf("imscore: invalid SIP transaction Via: %w", err)
	}
	return sipTransactionKey{
		CallID: strings.TrimSpace(callID), CSeq: cseq,
		Method: strings.ToUpper(method), Branch: branch,
	}, nil
}
