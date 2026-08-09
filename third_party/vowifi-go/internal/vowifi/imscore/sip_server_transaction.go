package imscore

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
)

type serverTransactionTimers struct {
	t1, t2, h, i, j, l, trying time.Duration
}

func defaultServerTransactionTimers() serverTransactionTimers {
	return serverTransactionTimers{
		t1: 500 * time.Millisecond, t2: 4 * time.Second,
		h: 32 * time.Second, i: 5 * time.Second,
		j: 32 * time.Second, l: 32 * time.Second,
		trying: 200 * time.Millisecond,
	}
}

type serverSIPTransaction struct {
	service  *Service
	key      string
	request  *sip.Request
	raw      string
	reply    func(string) error
	reliable bool
	invite   bool
	timers   serverTransactionTimers

	sendMu sync.Mutex
	mu     sync.Mutex
	last   string
	final  int
	closed bool
	err    error

	onTerminate sip.FnTxTerminate
	onCancel    sip.FnTxCancel

	acks     chan *sip.Request
	ack      chan struct{}
	done     chan struct{}
	doneOnce sync.Once
	trying   *time.Timer
}

func newServerSIPTransaction(
	service *Service,
	key string,
	request *sip.Request,
	raw string,
	reply func(string) error,
) *serverSIPTransaction {
	timers := service.serverTransactionTimers()
	transaction := &serverSIPTransaction{
		service: service, key: key, request: request.Clone(), raw: raw, reply: reply,
		reliable: sip.IsReliable(request.Transport()), invite: request.IsInvite(),
		timers: timers, acks: make(chan *sip.Request, 1), ack: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
	if transaction.invite {
		transaction.trying = time.AfterFunc(timers.trying, transaction.sendTrying)
	}
	return transaction
}

func (transaction *serverSIPTransaction) sendTrying() {
	response := buildInboundResponseFromRequest(transaction.request, 100, "Trying", nil, nil)
	_ = transaction.respond(response)
}

func (transaction *serverSIPTransaction) respond(response *sip.Response) error {
	if transaction == nil || response == nil {
		return errors.New("imscore: server transaction response is empty")
	}
	return transaction.respondWire(response.String(), response.StatusCode, response.Reason)
}

func (transaction *serverSIPTransaction) Respond(response *sip.Response) error {
	return transaction.respond(response)
}

func (transaction *serverSIPTransaction) respondWire(wire string, code int, reason string) error {
	transaction.sendMu.Lock()
	defer transaction.sendMu.Unlock()
	transaction.mu.Lock()
	if transaction.closed {
		transaction.mu.Unlock()
		return sip.ErrTransactionTerminated
	}
	if transaction.final >= 200 {
		transaction.mu.Unlock()
		return errors.New("imscore: server transaction final response already sent")
	}
	transaction.mu.Unlock()
	if err := transaction.write(wire, transaction.reply); err != nil {
		transaction.abort()
		return err
	}
	transaction.recordResponse(wire, code, reason)
	return nil
}

func (transaction *serverSIPTransaction) respondRaw(wire string) error {
	message, err := parseSIPMessage(wire)
	if err != nil {
		return fmt.Errorf("imscore: parse server transaction response: %w", err)
	}
	response, ok := message.(*sip.Response)
	if !ok {
		return errors.New("imscore: server transaction reply is not a SIP response")
	}
	return transaction.respond(response)
}

func (transaction *serverSIPTransaction) recordResponse(wire string, code int, reason string) {
	transaction.mu.Lock()
	transaction.last = wire
	if code >= 200 {
		transaction.final = code
	}
	trying := transaction.trying
	transaction.trying = nil
	transaction.mu.Unlock()
	if trying != nil {
		trying.Stop()
	}
	transaction.service.memoInboundRequestResponse(transaction.request, code, reason)
	if code >= 200 {
		go transaction.runFinalLifecycle(code)
	}
}

func (transaction *serverSIPTransaction) replay(reply func(string) error) error {
	transaction.mu.Lock()
	wire := transaction.last
	transaction.mu.Unlock()
	if wire == "" {
		return nil
	}
	return transaction.write(wire, reply)
}

func (transaction *serverSIPTransaction) write(wire string, reply func(string) error) error {
	if reply == nil {
		return errors.New("imscore: inbound SIP reply path is unavailable")
	}
	if err := reply(wire); err != nil {
		if transaction.service.transport != nil {
			transaction.service.transport.reportFatal(err)
		}
		return err
	}
	return nil
}

func (transaction *serverSIPTransaction) acceptACK(request *sip.Request) bool {
	transaction.mu.Lock()
	code := transaction.final
	transaction.mu.Unlock()
	if code < 300 {
		return false
	}
	select {
	case transaction.acks <- request.Clone():
	default:
	}
	select {
	case transaction.ack <- struct{}{}:
	default:
	}
	return true
}

func (transaction *serverSIPTransaction) acceptCancel(request *sip.Request) {
	transaction.mu.Lock()
	callback := transaction.onCancel
	transaction.mu.Unlock()
	if callback != nil {
		callback(request.Clone())
	}
}

func (transaction *serverSIPTransaction) abort() {
	transaction.fail(sip.ErrTransactionTransport, true)
}

func (transaction *serverSIPTransaction) fail(err error, releaseReservation bool) {
	transaction.mu.Lock()
	transaction.err = err
	transaction.mu.Unlock()
	transaction.finish(releaseReservation)
}

func (transaction *serverSIPTransaction) finish(releaseReservation bool) {
	transaction.doneOnce.Do(func() {
		transaction.mu.Lock()
		transaction.closed = true
		trying := transaction.trying
		transaction.trying = nil
		callback := transaction.onTerminate
		err := transaction.err
		transaction.mu.Unlock()
		if trying != nil {
			trying.Stop()
		}
		close(transaction.done)
		transaction.service.forgetServerTransactionByKey(transaction.key, transaction)
		if releaseReservation {
			transaction.service.releaseInboundRequestReservation(transaction.request)
		}
		if callback != nil {
			callback(transaction.key, err)
		}
	})
}

func (transaction *serverSIPTransaction) Terminate() {
	if transaction == nil {
		return
	}
	transaction.mu.Lock()
	transaction.err = sip.ErrTransactionTerminated
	transaction.mu.Unlock()
	transaction.finish(false)
}

func (transaction *serverSIPTransaction) OnTerminate(callback sip.FnTxTerminate) bool {
	if transaction == nil || callback == nil {
		return false
	}
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if transaction.closed {
		return false
	}
	previous := transaction.onTerminate
	transaction.onTerminate = callback
	if previous != nil {
		transaction.onTerminate = func(key string, err error) {
			previous(key, err)
			callback(key, err)
		}
	}
	return true
}

func (transaction *serverSIPTransaction) Done() <-chan struct{} {
	if transaction == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return transaction.done
}

func (transaction *serverSIPTransaction) Err() error {
	if transaction == nil {
		return sip.ErrTransactionTerminated
	}
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	return transaction.err
}

func (transaction *serverSIPTransaction) Acks() <-chan *sip.Request {
	if transaction == nil {
		return nil
	}
	return transaction.acks
}

func (transaction *serverSIPTransaction) OnCancel(callback sip.FnTxCancel) bool {
	if transaction == nil || callback == nil {
		return false
	}
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if transaction.closed {
		return false
	}
	previous := transaction.onCancel
	transaction.onCancel = callback
	if previous != nil {
		transaction.onCancel = func(request *sip.Request) {
			previous(request)
			callback(request)
		}
	}
	return true
}

var _ sip.ServerTransaction = (*serverSIPTransaction)(nil)

func (s *Service) serverTransactionTimers() serverTransactionTimers {
	s.serverTxMu.Lock()
	defer s.serverTxMu.Unlock()
	if s.serverTimers.t1 <= 0 {
		s.serverTimers = defaultServerTransactionTimers()
	}
	return s.serverTimers
}
