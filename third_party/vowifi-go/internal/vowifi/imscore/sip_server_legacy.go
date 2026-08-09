package imscore

import (
	"context"
	"errors"
	"strings"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

func newInboundRequestHandle(
	request *sip.Request,
	transaction sip.ServerTransaction,
) *imscoreInboundRequestHandle {
	id := inboundRequestKey(request)
	if id == "" {
		id = requestCallID(request)
	}
	runtime, _ := transaction.(*serverSIPTransaction)
	return &imscoreInboundRequestHandle{
		id: id, req: cloneSIPRequest(request), tx: transaction, runtime: runtime,
	}
}

func newServerInviteHandle(
	request *sip.Request,
	transaction sip.ServerTransaction,
) *imscoreServerInviteHandle {
	id := requestCallID(request)
	if id == "" {
		id = inboundRequestKey(request)
	}
	runtime, _ := transaction.(*serverSIPTransaction)
	return &imscoreServerInviteHandle{
		id: id, req: cloneSIPRequest(request), tx: transaction, runtime: runtime,
	}
}

func cloneSIPRequest(request *sip.Request) *sip.Request {
	if request == nil {
		return nil
	}
	return request.Clone()
}

func (handle *imscoreServerInviteHandle) markResponding() error {
	if handle == nil {
		return errors.New("server INVITE handle 为空")
	}
	handle.mu.Lock()
	defer handle.mu.Unlock()
	if handle.responded {
		return errors.New("server INVITE 已响应")
	}
	handle.responded = true
	return nil
}

func (s *Service) RespondInboundRequest(
	ctx context.Context,
	_ string,
	inbound imsendpoint.InboundRequestHandle,
	options imsendpoint.InboundResponseOptions,
) error {
	handle, ok := inbound.(*imscoreInboundRequestHandle)
	if !ok || handle == nil {
		return errors.New("inbound request handle 类型无效")
	}
	return s.respondInboundRequestWithOptions(ctx, handle, options)
}

// RespondInboundRequestForCurrentDevice retains the additive device-implicit API.
func (s *Service) RespondInboundRequestForCurrentDevice(
	ctx context.Context,
	inbound imsendpoint.InboundRequestHandle,
	options imsendpoint.InboundResponseOptions,
) error {
	return s.RespondInboundRequest(ctx, s.DeviceID(), inbound, options)
}

func (s *Service) respondInboundRequestWithOptions(
	ctx context.Context,
	handle *imscoreInboundRequestHandle,
	options imsendpoint.InboundResponseOptions,
) error {
	if handle == nil || handle.req == nil {
		return errors.New("inbound request 为空")
	}
	if handle.tx == nil {
		return errors.New("inbound server transaction 为空")
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	handle.mu.Lock()
	if handle.responded {
		handle.mu.Unlock()
		return errors.New("inbound request 已响应")
	}
	handle.mu.Unlock()
	code, reason := sanitizeInboundResponse(options.Code, options.Reason)
	_, err := s.sendInboundSIPResponse(
		handle.req.Clone(), code, reason, options.Body, options.Headers, handle.tx.Respond,
	)
	if err != nil {
		return err
	}
	handle.mu.Lock()
	handle.responded = true
	handle.mu.Unlock()
	s.memoInboundRequestResponse(handle.req, code, reason)
	return nil
}

func (s *Service) AnswerServerInvite(
	ctx context.Context,
	_ string,
	invite imsendpoint.ServerInviteHandle,
	options imsendpoint.ServerInviteAnswerOptions,
) (imsendpoint.DialogHandle, error) {
	handle, err := validServerInviteHandle(ctx, invite)
	if err != nil {
		return nil, err
	}
	if options.Response == nil {
		return nil, errors.New("server INVITE answer response 为空")
	}
	if options.Contact == nil {
		return nil, errors.New("server INVITE answer Contact 为空")
	}
	response := options.Response.Clone()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, errors.New("server INVITE answer 必须是 2xx response")
	}
	if response.Contact() == nil {
		response.AppendHeader(sip.HeaderClone(options.Contact))
	}
	if err := handle.markResponding(); err != nil {
		return nil, err
	}
	if err := handle.tx.Respond(response); err != nil {
		return nil, err
	}
	dialog := newServerDialogHandle(handle.req, response)
	dialog.inviteTx = handle.tx
	s.storeServerDialog(dialog, handle.req)
	return dialog, nil
}

// AnswerServerInviteForCurrentDevice retains the additive device-implicit API.
func (s *Service) AnswerServerInviteForCurrentDevice(
	ctx context.Context,
	invite imsendpoint.ServerInviteHandle,
	options imsendpoint.ServerInviteAnswerOptions,
) (imsendpoint.DialogHandle, error) {
	return s.AnswerServerInvite(ctx, s.DeviceID(), invite, options)
}

func (s *Service) RejectServerInvite(
	ctx context.Context,
	_ string,
	invite imsendpoint.ServerInviteHandle,
	options imsendpoint.ServerInviteRejectOptions,
) error {
	handle, err := validServerInviteHandle(ctx, invite)
	if err != nil {
		return err
	}
	response := options.Response
	if response == nil {
		code := options.Code
		if code < 1 {
			code = 603
		}
		reason := strings.TrimSpace(options.Reason)
		response = buildInboundResponseFromRequest(
			handle.req, code, reason, options.Body, options.Header,
		)
	} else {
		response = response.Clone()
	}
	if response.StatusCode >= 200 {
		if err := handle.markResponding(); err != nil {
			return err
		}
	}
	return handle.tx.Respond(response)
}

// RejectServerInviteForCurrentDevice retains the additive device-implicit API.
func (s *Service) RejectServerInviteForCurrentDevice(
	ctx context.Context,
	invite imsendpoint.ServerInviteHandle,
	options imsendpoint.ServerInviteRejectOptions,
) error {
	return s.RejectServerInvite(ctx, s.DeviceID(), invite, options)
}

func validServerInviteHandle(
	ctx context.Context,
	invite imsendpoint.ServerInviteHandle,
) (*imscoreServerInviteHandle, error) {
	handle, ok := invite.(*imscoreServerInviteHandle)
	if !ok || handle == nil {
		return nil, errors.New("server INVITE handle 类型无效")
	}
	if handle.req == nil {
		return nil, errors.New("server INVITE request 为空")
	}
	if handle.tx == nil {
		return nil, errors.New("server INVITE transaction 为空")
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return handle, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func sanitizeInboundResponse(code int, reason string) (int, string) {
	if code < 1 {
		code = 200
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = SIPStatusText(code)
	}
	if reason == "" {
		reason = "OK"
	}
	return code, reason
}

func newServerDialogHandle(request *sip.Request, response *sip.Response) *imscoreDialogHandle {
	id, err := sip.DialogIDFromResponse(response)
	if err != nil {
		id = sip.DialogIDMake(requestCallID(request), toHeaderTag(response.To()), fromHeaderTag(request.From()))
	}
	session := &sipgo.DialogServerSession{Dialog: sipgo.Dialog{
		ID: id, InviteRequest: request.Clone(), InviteResponse: response.Clone(),
	}}
	session.InitWithState(sip.DialogStateEstablished)
	handle := &imscoreDialogHandle{
		id: id, server: session, callID: requestCallID(request),
		fromTag: toHeaderTag(response.To()), toTag: fromHeaderTag(request.From()),
		inviteRequest: request.Clone(), inviteResponse: response.Clone(),
		confirmedCh: make(chan struct{}),
	}
	initializeServerDialogTarget(handle, request, response)
	return handle
}

func initializeServerDialogTarget(
	handle *imscoreDialogHandle,
	request *sip.Request,
	response *sip.Response,
) {
	if request.CSeq() != nil {
		handle.localCSeq = request.CSeq().SeqNo
		handle.remoteCSeq = request.CSeq().SeqNo
	}
	if response.Contact() != nil {
		handle.localContact = response.Contact().Clone()
	}
	if request.Contact() != nil {
		handle.remoteTarget = *request.Contact().Address.Clone()
	} else if request.From() != nil {
		handle.remoteTarget = *request.From().Address.Clone()
	}
}

func (s *Service) storeServerDialog(dialog *imscoreDialogHandle, request *sip.Request) {
	if s == nil || dialog == nil || request == nil {
		return
	}
	routes := request.GetHeaders("Record-Route")
	routeSet := make([]string, 0, len(routes))
	for _, route := range routes {
		routeSet = append(routeSet, route.Value())
	}
	cseq := 0
	if request.CSeq() != nil {
		cseq = int(request.CSeq().SeqNo)
	}
	dialog.routeSet = routeSet
	dialog.localCSeq = uint32(cseq)
	s.dialogs().store(dialog)
}

func (s *Service) sendInboundSIPResponse(
	request *sip.Request,
	code int,
	reason string,
	body []byte,
	headers []sip.Header,
	writer func(*sip.Response) error,
) (*sip.Response, error) {
	if request == nil {
		return nil, errors.New("inbound request 为空")
	}
	code, reason = sanitizeInboundResponse(code, reason)
	response := buildInboundResponseFromRequest(request, code, reason, body, headers)
	if writer != nil {
		return response, writer(response)
	}
	transaction := s.serverTransactionByKey(serverTransactionKey(request, false))
	if transaction == nil {
		return nil, errors.New("inbound server transaction 不存在")
	}
	return response, transaction.Respond(response)
}

func (s *Service) respondInboundRequest(
	request *sip.Request,
	code int,
	reason string,
	body []byte,
	headers []sip.Header,
	reply func(string) error,
) error {
	var writer func(*sip.Response) error
	if reply != nil {
		writer = func(response *sip.Response) error { return reply(response.String()) }
	}
	_, err := s.sendInboundSIPResponse(request, code, reason, body, headers, writer)
	if err != nil {
		s.releaseInboundRequestReservation(request)
		if s.transport != nil {
			s.transport.reportFatal(err)
		}
		return err
	}
	code, reason = sanitizeInboundResponse(code, reason)
	s.memoInboundRequestResponse(request, code, reason)
	return nil
}

// AnswerServerInviteRaw retains the previous handle-only compatibility helper.
func (s *Service) AnswerServerInviteRaw(handle *imscoreServerInviteHandle) error {
	if handle == nil || handle.req == nil {
		return errors.New("imscore: server INVITE request context is unavailable")
	}
	return errors.New("imscore: answer response and Contact are required")
}

// RejectServerInviteRaw retains the previous handle-only compatibility helper.
func (s *Service) RejectServerInviteRaw(handle *imscoreServerInviteHandle) error {
	return s.RejectServerInvite(context.Background(), s.DeviceID(), handle, imsendpoint.ServerInviteRejectOptions{})
}

// RespondInboundRequestRaw retains the previous status-only compatibility helper.
func (s *Service) RespondInboundRequestRaw(handle *imscoreInboundRequestHandle, status int) error {
	return s.RespondInboundRequest(
		context.Background(), s.DeviceID(), handle, imsendpoint.InboundResponseOptions{Code: status},
	)
}
