package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type sipTransactionKey struct {
	CallID string
	CSeq   int
	Method string
	Branch string
}

func (t *sipTransport) RoundTrip(ctx context.Context, request string) (*sipResponse, error) {
	if t == nil {
		return nil, errors.New("imscore: nil SIP transport")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key, err := transactionKeyFromRequest(request)
	if err != nil {
		return nil, err
	}
	waiter := make(chan *sipResponse, 8)
	if err := t.addWaiter(key, waiter); err != nil {
		return nil, err
	}
	defer t.removeWaiter(key, waiter)
	if err := t.Send(request); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.closed:
			return nil, errors.New("imscore: SIP transport closed")
		case response := <-waiter:
			if response != nil && response.StatusCode >= 200 {
				return response, nil
			}
		}
	}
}

func (t *sipTransport) addWaiter(key sipTransactionKey, waiter chan *sipResponse) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case <-t.closed:
		return errors.New("imscore: SIP transport closed")
	default:
	}
	if _, exists := t.waiters[key]; exists {
		return fmt.Errorf("imscore: duplicate SIP transaction %+v", key)
	}
	t.waiters[key] = waiter
	return nil
}

func (t *sipTransport) removeWaiter(key sipTransactionKey, waiter chan *sipResponse) {
	t.mu.Lock()
	if t.waiters[key] == waiter {
		delete(t.waiters, key)
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
