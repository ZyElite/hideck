package ussi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

const ussiTransactionTimeout = 45 * time.Second

// Send starts a USSI dialog and waits for a network-provided result.
func (s *Service) Send(ctx context.Context, command string) (*Result, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, errors.New("USSD command 为空")
	}
	endpoint, err := s.readyEndpoint()
	if err != nil {
		return nil, err
	}
	body, err := EncodeXML(command, defaultLanguage)
	if err != nil {
		return nil, err
	}
	dialogContext, err := contextFromEndpoint(endpoint)
	if err != nil {
		return nil, fmt.Errorf("构建 USSI 上下文失败: %w", err)
	}
	session, request, err := s.startSession(endpoint, dialogContext, command, body)
	if err != nil {
		return nil, err
	}
	logUSSISIPRaw(s.deviceID, "initial_invite", "send", request)
	invite, err := endpoint.StartClientInvite(operationContext(ctx), s.deviceID,
		imsendpoint.ClientInviteOptions{
			Request: request, Contact: request.Contact(), Timeout: int64(ussiTransactionTimeout),
		})
	if invite != nil {
		logUSSISIPRaw(s.deviceID, "initial_invite", "recv", invite.Response)
	}
	if err != nil {
		return s.failInvite(session, invite, err)
	}
	if err := s.establishDialog(ctx, session, invite); err != nil {
		return s.failSession(session, err)
	}
	result, err := parseOrWaitResult(ctx, invite.Response, session)
	return s.finishResult(session, result, err)
}

// SendContext is the additive spelling retained from the reconstruction.
func (s *Service) SendContext(ctx context.Context, command string) (*Result, error) {
	return s.Send(ctx, command)
}

func (s *Service) startSession(
	endpoint imsendpoint.ClientDialogEndpoint,
	dialogContext Context,
	command string,
	body []byte,
) (*Session, *sip.Request, error) {
	now := time.Now()
	session := &Session{
		ID: "ussd-" + common.RandomHex(8), CallID: common.RandomHex(32),
		State: sessionActive, ResultCh: make(chan InfoResult, 4),
		CreatedAt: now, LastAt: now, dialogContext: dialogContext,
	}
	cseq := endpoint.NextCSeq()
	if cseq == 0 {
		cseq = 1
	}
	request, err := BuildInitialInvite(
		dialogContext, command, session.CallID, common.RandomHex(8),
		"z9hG4bK"+common.RandomHex(18), cseq, body,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("构建 USSI INVITE 失败: %w", err)
	}
	session.RemoteURI = request.Recipient.String()
	session.RemoteTarget = session.RemoteURI
	s.setSession(session)
	if !s.ownsSession(session) {
		return nil, nil, errors.New("已有活动 USSD 会话，请先继续或取消当前会话")
	}
	return session, request, nil
}

func (s *Service) establishDialog(
	ctx context.Context,
	session *Session,
	invite *imsendpoint.ClientInviteResult,
) error {
	if invite == nil || invite.Response == nil {
		return errors.New("USSI INVITE final response 为空")
	}
	if invite.Dialog == nil {
		return errors.New("USSI INVITE 未建立 dialog")
	}
	learnSessionFromResponse(session, invite)
	sendACK(ctx, s.deviceID, s.endpoint, invite.Dialog, session)
	return nil
}

func learnSessionFromResponse(session *Session, invite *imsendpoint.ClientInviteResult) {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.dialogHandle = invite.Dialog
	if contact := invite.Response.Contact(); contact != nil {
		session.RemoteTarget = contact.Address.String()
	}
	session.LastAt = time.Now()
}

func (s *Service) failInvite(
	session *Session,
	invite *imsendpoint.ClientInviteResult,
	cause error,
) (*Result, error) {
	text := cause.Error()
	if invite != nil && invite.Response != nil {
		text = fmt.Sprintf("%d %s", invite.Response.StatusCode, invite.Response.Reason)
	}
	return s.failSession(session, fmt.Errorf("USSI INVITE 失败: %w", cause), text)
}

func (s *Service) failSession(session *Session, cause error, text ...string) (*Result, error) {
	message := cause.Error()
	if len(text) > 0 && strings.TrimSpace(text[0]) != "" {
		message = strings.TrimSpace(text[0])
	}
	result := &Result{Text: message, Status: 2, SessionID: session.ID, DCS: defaultDCS}
	s.closeSession(session)
	return result, cause
}

// Continue sends an in-dialog INFO and waits for its result.
func (s *Service) Continue(
	ctx context.Context,
	sessionID, input string,
) (*Result, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, errors.New("USSD 输入为空")
	}
	session, err := s.sessionFor(sessionID)
	if err != nil {
		return nil, err
	}
	endpoint, err := s.readyEndpoint()
	if err != nil {
		return nil, err
	}
	body, err := EncodeXML(input, defaultLanguage)
	if err != nil {
		return nil, err
	}
	dialog, dialogContext, ok := sessionDialog(session)
	if !ok {
		return nil, errors.New("USSI 会话缺少 dialog handle")
	}
	touchSession(session)
	request, err := BuildInfo(session, body, dialogContext)
	if err != nil {
		return nil, fmt.Errorf("构建 USSI INFO 失败: %w", err)
	}
	logUSSISIPRaw(s.deviceID, "continue_info", "send", request)
	response, err := endpoint.SendDialogRequest(
		operationContext(ctx), s.deviceID, dialog, request,
		imsendpoint.DialogRequestOptions{Timeout: int64(ussiTransactionTimeout)},
	)
	logUSSISIPRaw(s.deviceID, "continue_info", "recv", response)
	if err != nil {
		return s.failSession(session, fmt.Errorf("USSI INFO 失败: %w", err))
	}
	result, err := parseOrWaitResult(ctx, response, session)
	return s.finishResult(session, result, err)
}

// ContinueContext retains the additive reconstruction API.
func (s *Service) ContinueContext(
	ctx context.Context,
	sessionID, input string,
) (*Result, error) {
	return s.Continue(ctx, sessionID, input)
}

// Cancel sends BYE and clears the active session even when BYE fails.
func (s *Service) Cancel(ctx context.Context, sessionID string) error {
	session, err := s.sessionFor(sessionID)
	if err != nil {
		return err
	}
	endpoint, err := s.readyEndpoint()
	if err != nil {
		return err
	}
	dialog, dialogContext, ok := sessionDialog(session)
	if !ok {
		s.closeSession(session)
		return errors.New("USSI 会话缺少 dialog handle")
	}
	touchSession(session)
	request, err := buildDialogRequest(session, sip.BYE, nil, dialogContext)
	if err != nil {
		s.closeSession(session)
		return fmt.Errorf("构建 USSI BYE 失败: %w", err)
	}
	logUSSISIPRaw(s.deviceID, "cancel_bye", "send", request)
	response, sendErr := endpoint.SendDialogRequest(
		operationContext(ctx), s.deviceID, dialog, request,
		imsendpoint.DialogRequestOptions{Timeout: int64(ussiTransactionTimeout)},
	)
	logUSSISIPRaw(s.deviceID, "cancel_bye", "recv", response)
	closeErr := endpoint.CloseDialog(operationContext(ctx), s.deviceID, dialog)
	s.clearSession(session.ID)
	if sendErr != nil {
		sendErr = fmt.Errorf("发送 USSI BYE 失败: %w", sendErr)
	}
	return errors.Join(sendErr, closeErr)
}

// CancelContext retains the additive reconstruction API.
func (s *Service) CancelContext(ctx context.Context, sessionID string) error {
	return s.Cancel(ctx, sessionID)
}

// ActiveSessionID returns the active menu session identifier.
func (s *Service) ActiveSessionID() string {
	session := s.activeSession()
	if session == nil {
		return ""
	}
	return session.ID
}

func (s *Service) readyEndpoint() (imsendpoint.ClientDialogEndpoint, error) {
	if s == nil || s.endpoint == nil {
		return nil, errors.New("USSI endpoint 为空")
	}
	if !s.endpoint.IsRegistered() {
		return nil, errors.New("IMS 未注册，无法发送 USSD")
	}
	return s.endpoint, nil
}
func operationContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
