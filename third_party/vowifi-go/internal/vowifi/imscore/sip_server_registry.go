package imscore

import (
	"errors"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
)

type trackedServerTransaction struct {
	tx     sip.ServerTransaction
	at     time.Time
	callID string
	cseq   string

	runtime *serverSIPTransaction
}

func (s *Service) acceptServerRequest(
	request *sip.Request,
	raw string,
	reply func(string) error,
) (*serverSIPTransaction, bool, error) {
	if request == nil {
		return nil, false, errors.New("imscore: inbound SIP request is empty")
	}
	if request.IsAck() {
		return s.acceptServerACK(request)
	}
	if request.IsCancel() {
		s.notifyCanceledInvite(request)
	}

	transaction := newServerSIPTransaction(s, serverTransactionKey(request, false), request, raw, reply)
	key := transaction.key
	if key == "" {
		return s.acceptUntrackedServerRequest(transaction)
	}

	existing, reserved, memo := s.rememberServerTransaction(request, transaction)
	if existing != nil {
		transaction.finish(false)
		return nil, true, existing.replay(reply)
	}
	if !reserved {
		transaction.finish(false)
		return nil, true, s.replayInboundRequestMemo(request, memo, reply)
	}
	return transaction, false, nil
}

func (s *Service) acceptUntrackedServerRequest(
	transaction *serverSIPTransaction,
) (*serverSIPTransaction, bool, error) {
	reserved, memo := s.reserveInboundRequestWithMemo(transaction.request)
	if reserved {
		return transaction, false, nil
	}
	transaction.finish(false)
	return nil, true, s.replayInboundRequestMemo(transaction.request, memo, transaction.reply)
}

func (s *Service) acceptServerACK(request *sip.Request) (*serverSIPTransaction, bool, error) {
	key := serverTransactionKey(request, true)
	transaction := s.serverTransactionByKey(key)
	if transaction == nil {
		return nil, false, nil
	}
	if transaction.acceptACK(request) {
		return nil, true, nil
	}
	return nil, false, nil
}

func (s *Service) notifyCanceledInvite(request *sip.Request) {
	key := serverTransactionKey(request, true)
	if transaction := s.serverTransactionByKey(key); transaction != nil {
		transaction.acceptCancel(request)
	}
}

func (s *Service) serverTransactionByKey(key string) *serverSIPTransaction {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	s.serverTxMu.Lock()
	transaction := s.serverTx[key].runtime
	s.serverTxMu.Unlock()
	return transaction
}

func (s *Service) rememberServerTransaction(
	request *sip.Request,
	tx sip.ServerTransaction,
) (*serverSIPTransaction, bool, *inboundRequestResponseMemo) {
	key := serverTransactionKey(request, false)
	if key == "" || tx == nil {
		return nil, true, nil
	}
	runtime, _ := tx.(*serverSIPTransaction)
	s.serverTxMu.Lock()
	s.ensureServerTransactionMapLocked()
	if existing := s.serverTx[key].runtime; existing != nil {
		s.serverTxMu.Unlock()
		return existing, false, nil
	}
	reserved, memo := s.reserveInboundRequestWithMemo(request)
	if !reserved {
		s.serverTxMu.Unlock()
		return nil, false, memo
	}
	s.serverTx[key] = trackedServerTransaction{
		tx: tx, at: time.Now(), callID: requestCallID(request),
		cseq: requestCSeq(request), runtime: runtime,
	}
	s.serverTxMu.Unlock()
	if runtime == nil {
		go func() {
			<-tx.Done()
			s.forgetTrackedServerTransaction(key, tx)
		}()
	}
	return nil, true, nil
}

func (s *Service) clearServerTransactions() int {
	s.serverTxMu.Lock()
	transactions := make([]sip.ServerTransaction, 0, len(s.serverTx))
	for _, tracked := range s.serverTx {
		if tracked.tx != nil {
			transactions = append(transactions, tracked.tx)
		}
	}
	count := len(s.serverTx)
	s.serverTx = make(map[string]trackedServerTransaction, 128)
	s.serverTxMu.Unlock()
	for _, transaction := range transactions {
		transaction.Terminate()
	}
	return count
}

func (s *Service) forgetTrackedServerTransaction(key string, transaction sip.ServerTransaction) {
	s.serverTxMu.Lock()
	tracked, exists := s.serverTx[key]
	if exists && tracked.tx != nil && tracked.tx.Done() == transaction.Done() {
		delete(s.serverTx, key)
	}
	s.serverTxMu.Unlock()
}

func (s *Service) forgetServerTransactionByKey(key string, transaction *serverSIPTransaction) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	s.serverTxMu.Lock()
	tracked, exists := s.serverTx[key]
	if exists && (transaction == nil || tracked.runtime == transaction) {
		delete(s.serverTx, key)
	}
	s.serverTxMu.Unlock()
}

func (s *Service) replayInboundRequestMemo(
	request *sip.Request,
	memo *inboundRequestResponseMemo,
	reply func(string) error,
) error {
	if reply == nil {
		return errors.New("imscore: inbound SIP reply path is unavailable")
	}
	if memo == nil {
		memo = &inboundRequestResponseMemo{Code: 200, Reason: "OK", At: time.Now()}
	}
	code, reason := memo.Sanitize()
	return s.respondInboundRequest(request, code, reason, nil, nil, reply)
}

func (s *Service) ensureServerTransactionMapLocked() {
	if s.serverTx == nil {
		s.serverTx = make(map[string]trackedServerTransaction, 128)
	}
}

func requestCallID(request *sip.Request) string {
	if request == nil || request.CallID() == nil {
		return ""
	}
	return strings.TrimSpace(request.CallID().Value())
}

func requestCSeq(request *sip.Request) string {
	if request == nil || request.CSeq() == nil {
		return ""
	}
	return strings.TrimSpace(request.CSeq().Value())
}
