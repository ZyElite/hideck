package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

// StartClientInvite starts a v1.5.5 client-side INVITE transaction.
func (s *Service) StartClientInvite(
	ctx context.Context,
	_ string,
	options imsendpoint.ClientInviteOptions,
) (*imsendpoint.ClientInviteResult, error) {
	request, err := prepareClientInviteRequest(options)
	if err != nil {
		return nil, err
	}
	if s == nil || !s.IsRegistered() {
		return nil, errors.New("client INVITE 仅可在 IMS 注册成功后发送")
	}
	ctx, cancel := clientInviteContext(ctx, time.Duration(options.Timeout))
	defer cancel()
	var handle *imscoreInviteHandle
	callbacks := sipTransactionCallbacks{
		onProvisional: func(response *sipResponse) error {
			s.retainClientInviteEarlyDialog(handle, response)
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
	handle = newClientInviteHandle(transaction)
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

// StartClientInviteForCurrentDevice retains the additive device-implicit API.
func (s *Service) StartClientInviteForCurrentDevice(
	ctx context.Context,
	options imsendpoint.ClientInviteOptions,
) (*imsendpoint.ClientInviteResult, error) {
	return s.StartClientInvite(ctx, s.DeviceID(), options)
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
			s.closeClientInviteDialog(handle)
			return result, errors.Join(waitErr, err)
		}
	}
	accepted := response != nil && response.StatusCode >= 200 && response.StatusCode < 300
	if accepted {
		if err := s.completeAcceptedClientInvite(handle, result, response); err != nil {
			return result, errors.Join(waitErr, err)
		}
		return result, waitErr
	}
	handle.markDone(false)
	s.closeClientInviteDialog(handle)
	if waitErr != nil {
		return result, waitErr
	}
	if response == nil {
		return result, errors.New("client INVITE final response 为空")
	}
	return result, fmt.Errorf("client INVITE response: %d %s", response.StatusCode, response.Reason)
}

func (s *Service) completeAcceptedClientInvite(
	handle *imscoreInviteHandle,
	result *imsendpoint.ClientInviteResult,
	response *sipResponse,
) error {
	if response == nil || response.parsed == nil {
		handle.markDone(false)
		s.closeClientInviteDialog(handle)
		return errors.New("client INVITE final response 未解析")
	}
	dialog := s.promoteClientInviteDialog(handle, response.parsed)
	handle.markDone(true)
	handle.mu.Lock()
	handle.dialog = dialog.client
	handle.mu.Unlock()
	result.Dialog = dialog
	s.storeClientDialog(dialog, handle.initialRequest, response.parsed)
	return nil
}

func (s *Service) promoteClientInviteDialog(
	invite *imscoreInviteHandle,
	response *sip.Response,
) *imscoreDialogHandle {
	id, _ := sip.DialogIDFromResponse(response)
	earlyID := clientInviteDialogID(invite)
	dialog := s.dialogs().load(id)
	if dialog == nil {
		dialog = newClientDialogHandle(invite.initialRequest, response)
		dialog.sender = invite.transaction.send
		s.storeClientDialog(dialog, invite.initialRequest, response)
		s.closeReplacedEarlyDialog(earlyID, dialog.id)
		return dialog
	}
	dialog.mu.Lock()
	dialog.inviteResponse = response.Clone()
	dialog.client.InviteResponse = response.Clone()
	dialog.confirmed = true
	if response.Contact() != nil {
		dialog.remoteTarget = *response.Contact().Address.Clone()
	}
	dialog.mu.Unlock()
	dialog.client.InitWithState(sip.DialogStateConfirmed)
	s.storeClientDialog(dialog, invite.initialRequest, response)
	s.closeReplacedEarlyDialog(earlyID, dialog.id)
	return dialog
}

func clientInviteDialogID(invite *imscoreInviteHandle) string {
	if invite == nil {
		return ""
	}
	invite.mu.Lock()
	defer invite.mu.Unlock()
	if invite.dialog == nil {
		return ""
	}
	return invite.dialog.ID
}

func (s *Service) closeReplacedEarlyDialog(previousID, currentID string) {
	if previousID == "" || previousID == currentID {
		return
	}
	if previous := s.dialogs().load(previousID); previous != nil {
		_ = s.closeDialogHandle(previous)
	}
}

func (s *Service) closeClientInviteDialog(invite *imscoreInviteHandle) {
	if invite == nil {
		return
	}
	invite.mu.Lock()
	session := invite.dialog
	invite.dialog = nil
	invite.mu.Unlock()
	if session == nil {
		return
	}
	dialog := s.dialogs().load(session.ID)
	if dialog != nil {
		_ = s.closeDialogHandle(dialog)
	}
}

func (s *Service) retainClientInviteEarlyDialog(
	invite *imscoreInviteHandle,
	response *sipResponse,
) {
	if invite == nil || response == nil || response.parsed == nil || response.StatusCode <= 100 {
		return
	}
	if response.parsed.To() == nil || toHeaderTag(response.parsed.To()) == "" {
		return
	}
	dialog := newClientDialogHandle(invite.initialRequest, response.parsed)
	dialog.client.Init()
	dialog.confirmed = false
	dialog.sender = invite.transaction.send
	s.storeClientDialog(dialog, invite.initialRequest, response.parsed)
	invite.mu.Lock()
	previous := invite.dialog
	invite.dialog = dialog.client
	invite.mu.Unlock()
	if previous != nil {
		s.closeReplacedEarlyDialog(previous.ID, dialog.id)
	}
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
	id, err := sip.DialogIDFromResponse(response)
	if err != nil {
		id = sip.DialogIDMake(request.CallID().Value(), toHeaderTag(response.To()), fromHeaderTag(request.From()))
	}
	session := &sipgo.DialogClientSession{Dialog: sipgo.Dialog{
		ID: id, InviteRequest: request.Clone(), InviteResponse: response.Clone(),
	}}
	session.InitWithState(sip.DialogStateConfirmed)
	handle := &imscoreDialogHandle{
		id: id, client: session, callID: request.CallID().Value(),
		fromTag: fromHeaderTag(request.From()), toTag: toHeaderTag(response.To()),
		inviteRequest: request.Clone(), inviteResponse: response.Clone(), confirmed: true,
	}
	initializeClientDialogTarget(handle, request, response)
	return handle
}

func initializeClientDialogTarget(
	handle *imscoreDialogHandle,
	request *sip.Request,
	response *sip.Response,
) {
	if request.CSeq() != nil {
		handle.localCSeq = request.CSeq().SeqNo
		handle.remoteCSeq = request.CSeq().SeqNo
	}
	if request.Contact() != nil {
		handle.localContact = request.Contact().Clone()
	}
	if response.Contact() != nil {
		handle.remoteTarget = *response.Contact().Address.Clone()
	} else {
		handle.remoteTarget = *request.Recipient.Clone()
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
	if s == nil || dialog == nil {
		return
	}
	routes := response.GetHeaders("Record-Route")
	routeSet := make([]string, 0, len(routes))
	for index := len(routes) - 1; index >= 0; index-- {
		routeSet = append(routeSet, routes[index].Value())
	}
	dialog.mu.Lock()
	dialog.routeSet = routeSet
	dialog.mu.Unlock()
	s.dialogs().store(dialog)
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
