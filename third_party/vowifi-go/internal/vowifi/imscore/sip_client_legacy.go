package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

// StartClientInvite starts a v1.5.5 client-side INVITE transaction.
func (s *Service) StartClientInvite(
	ctx context.Context,
	options imsendpoint.ClientInviteOptions,
) (*imsendpoint.ClientInviteResult, error) {
	request, err := prepareClientInviteRequest(options)
	if err != nil {
		return nil, err
	}
	if s == nil || !s.IsRegistered() {
		return nil, errors.New("client INVITE 仅可在 IMS 注册成功后发送")
	}
	ctx, cancel := clientInviteContext(ctx, options.Timeout)
	defer cancel()
	callbacks := sipTransactionCallbacks{
		onProvisional: func(response *sipResponse) error {
			return callClientInviteResponseHandler(options.OnResponse, response)
		},
		onFinalRetransmit: func(response *sipResponse) error {
			return callClientInviteResponseHandler(options.OnResponse, response)
		},
	}
	transaction, err := s.startClientInviteTransaction(request, callbacks)
	if err != nil {
		return nil, err
	}
	handle := newClientInviteHandle(transaction)
	result := &imsendpoint.ClientInviteResult{InviteHandle: handle}
	if options.OnStarted != nil {
		if err := options.OnStarted(handle); err != nil {
			s.transport.removeTransaction(transaction)
			transaction.finish()
			handle.markDone(false)
			return result, err
		}
	}
	response, err := s.transport.waitClientTransaction(ctx, transaction)
	return s.completeClientInviteResult(handle, result, response, options.OnResponse, err)
}

func (s *Service) startClientInviteTransaction(
	request *sip.Request,
	callbacks sipTransactionCallbacks,
) (*clientSIPTransaction, error) {
	if s == nil || s.transport == nil {
		return nil, errors.New("client INVITE SIP client 为空")
	}
	return s.transport.startClientTransaction(request.String(), callbacks)
}

func prepareClientInviteRequest(options imsendpoint.ClientInviteOptions) (*sip.Request, error) {
	if options.Request == nil {
		return nil, errors.New("client INVITE request 为空")
	}
	if !options.Request.IsInvite() {
		return nil, errors.New("client INVITE request method 不是 INVITE")
	}
	request := options.Request.Clone()
	if options.Contact != nil {
		request.ReplaceHeader(sip.HeaderClone(options.Contact))
	}
	if request.Contact() == nil {
		return nil, errors.New("client INVITE Contact 为空")
	}
	if request.CallID() == nil || strings.TrimSpace(request.CallID().Value()) == "" {
		return nil, errors.New("client INVITE Call-ID 为空")
	}
	return request, nil
}

func clientInviteContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return context.WithCancel(ctx)
}

func callClientInviteResponseHandler(
	handler func(*sip.Response) error,
	response *sipResponse,
) error {
	if handler == nil || response == nil || response.parsed == nil {
		return nil
	}
	return handler(response.parsed.Clone())
}

func newClientInviteHandle(transaction *clientSIPTransaction) *imscoreInviteHandle {
	return &imscoreInviteHandle{
		id:             transaction.parsed.CallID().Value(),
		initialRequest: transaction.parsed.Clone(),
		transaction:    transaction,
	}
}

func (s *Service) completeClientInviteResult(
	handle *imscoreInviteHandle,
	result *imsendpoint.ClientInviteResult,
	response *sipResponse,
	handler func(*sip.Response) error,
	waitErr error,
) (*imsendpoint.ClientInviteResult, error) {
	recoveredFinal := false
	if response == nil {
		response = retainedClientInviteFinal(handle)
		recoveredFinal = response != nil
	}
	if response != nil && response.parsed != nil {
		result.Response = response.parsed.Clone()
	}
	if response != nil && (!recoveredFinal || response.StatusCode >= 300) {
		if err := callClientInviteResponseHandler(handler, response); err != nil {
			handle.markDone(false)
			return result, errors.Join(waitErr, err)
		}
	}
	if waitErr != nil {
		handle.markDone(false)
		return result, waitErr
	}
	accepted := response != nil && response.StatusCode >= 200 && response.StatusCode < 300
	handle.markDone(accepted)
	if !accepted {
		return result, fmt.Errorf("client INVITE response: %d %s", response.StatusCode, response.Reason)
	}
	dialog := newClientDialogHandle(handle.initialRequest, response.parsed)
	result.Dialog = dialog
	s.storeClientDialog(dialog, handle.initialRequest, response.parsed)
	return result, nil
}

func retainedClientInviteFinal(handle *imscoreInviteHandle) *sipResponse {
	if handle == nil || handle.transaction == nil {
		return nil
	}
	handle.transaction.mu.Lock()
	defer handle.transaction.mu.Unlock()
	return handle.transaction.final
}

func newClientDialogHandle(request *sip.Request, response *sip.Response) *imscoreDialogHandle {
	return &imscoreDialogHandle{
		callID:  request.CallID().Value(),
		fromTag: fromHeaderTag(request.From()),
		toTag:   toHeaderTag(response.To()),
	}
}

func fromHeaderTag(header *sip.FromHeader) string {
	if header == nil {
		return ""
	}
	value, _ := header.Params.Get("tag")
	return value
}

func toHeaderTag(header *sip.ToHeader) string {
	if header == nil {
		return ""
	}
	value, _ := header.Params.Get("tag")
	return value
}

func (s *Service) storeClientDialog(
	dialog *imscoreDialogHandle,
	request *sip.Request,
	response *sip.Response,
) {
	if s == nil || s.dialogs == nil || dialog == nil {
		return
	}
	routes := response.GetHeaders("Record-Route")
	routeSet := make([]string, 0, len(routes))
	for index := len(routes) - 1; index >= 0; index-- {
		routeSet = append(routeSet, routes[index].Value())
	}
	s.dialogs.store(dialog.callID, &dialogEntry{
		handle: dialog, localTag: dialog.fromTag, remoteTag: dialog.toTag,
		cseq: int(request.CSeq().SeqNo), route: routeSet,
	})
}

func (s *Service) cancelClientInviteWithContext(
	ctx context.Context,
	handle *imscoreInviteHandle,
	_ imsendpoint.ClientInviteCancelOptions,
) error {
	transaction, err := beginClientInviteHandleCancel(s, handle)
	if err != nil {
		return err
	}
	cancelRequest, err := buildClientInviteCancelRequest(transaction.parsed.Clone())
	if err != nil {
		finishClientInviteHandleCancel(handle, transaction, false)
		return err
	}
	cancelTransaction, err := s.transport.startClientTransactionWithSender(
		cancelRequest.String(), sipTransactionCallbacks{}, transaction.send,
	)
	if err != nil {
		finishClientInviteHandleCancel(handle, transaction, false)
		return fmt.Errorf("client INVITE CANCEL: %w", err)
	}
	response, err := s.transport.waitClientTransaction(ctx, cancelTransaction)
	if err == nil && response.StatusCode != 200 {
		err = fmt.Errorf("client INVITE CANCEL response: %d %s", response.StatusCode, response.Reason)
	}
	finishClientInviteHandleCancel(handle, transaction, err == nil)
	return err
}

func beginClientInviteHandleCancel(
	s *Service,
	handle *imscoreInviteHandle,
) (*clientSIPTransaction, error) {
	if s == nil || s.transport == nil {
		return nil, errors.New("client INVITE CANCEL SIP client 为空")
	}
	handle.mu.Lock()
	defer handle.mu.Unlock()
	switch {
	case handle.confirmed:
		return nil, errors.New("client INVITE 已接通，不能 CANCEL")
	case handle.done:
		return nil, errors.New("client INVITE 已结束，不能 CANCEL")
	case handle.canceling || handle.cancelSent:
		return nil, errors.New("client INVITE 已在取消中")
	case handle.transaction == nil || handle.initialRequest == nil:
		return nil, errors.New("client INVITE initial request 为空")
	case !handle.transaction.beginCancel():
		return nil, errors.New("client INVITE 已在取消中")
	}
	handle.canceling = true
	return handle.transaction, nil
}

func finishClientInviteHandleCancel(
	handle *imscoreInviteHandle,
	transaction *clientSIPTransaction,
	succeeded bool,
) {
	if succeeded {
		transaction.cancelSucceeded()
	} else {
		transaction.cancelFailed()
	}
	handle.mu.Lock()
	handle.canceling = false
	handle.cancelSent = succeeded
	handle.mu.Unlock()
}
