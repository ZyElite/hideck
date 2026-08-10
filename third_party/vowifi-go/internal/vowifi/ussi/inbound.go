package ussi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
)

// HandleInboundInfoNoResponse consumes a matching network INFO.
func (s *Service) HandleInboundInfoNoResponse(_ context.Context, request *sip.Request) bool {
	if request == nil {
		return false
	}
	if request.Method != sip.INFO {
		s.logInboundMismatch("inbound_info", "method_mismatch", request)
		return false
	}
	if !IsContentType(requestHeader(request, "Content-Type")) {
		s.logInboundMismatch("inbound_info", "content_type_mismatch", request)
		return false
	}
	session, err := s.matchInboundSession(request)
	if err != nil {
		s.logInboundMismatch("inbound_info", err.Error(), request)
		return false
	}
	logUSSISIPRaw(s.deviceID, "inbound_info", "recv", request)
	result := parseInboundInfo(request.Body())
	session.mu.Lock()
	session.LastAt = time.Now()
	resultCh := session.ResultCh
	session.mu.Unlock()
	deliverInfoResult(resultCh, result)
	return true
}

// HandleInboundByeNoResponse consumes a matching network BYE.
func (s *Service) HandleInboundByeNoResponse(_ context.Context, request *sip.Request) bool {
	if request == nil {
		return false
	}
	if request.Method != sip.BYE {
		s.logInboundMismatch("inbound_bye", "method_mismatch", request)
		return false
	}
	session, err := s.matchInboundSession(request)
	if err != nil {
		s.logInboundMismatch("inbound_bye", err.Error(), request)
		return false
	}
	logUSSISIPRaw(s.deviceID, "inbound_bye", "recv", request)
	result := InfoResult{Err: errors.New("USSD 会话已被网络终止")}
	if len(request.Body()) > 0 && IsContentType(requestHeader(request, "Content-Type")) {
		result = parseInboundInfo(request.Body())
	}
	deliverInfoResult(session.ResultCh, result)
	s.clearSession(session.ID)
	return true
}

func deliverInfoResult(resultCh chan InfoResult, result InfoResult) {
	if resultCh == nil {
		return
	}
	select {
	case resultCh <- result:
	default:
	}
}

func parseInboundInfo(body []byte) InfoResult {
	xmlBody := ExtractFromMultipart(body)
	if len(xmlBody) == 0 {
		xmlBody = body
	}
	payload, err := DecodeXML(xmlBody)
	if err != nil {
		return InfoResult{RawXML: string(body), Err: fmt.Errorf("解析入站 USSI XML 失败: %w", err)}
	}
	return InfoResult{Text: payload.USSDString, RawXML: string(xmlBody)}
}

func parseOrWaitResult(ctx context.Context, response *sip.Response, session *Session) (*Result, error) {
	if responseCarriesUSSD(response) {
		return ParseResult(response.Body(), session.ID), nil
	}
	waitCtx, cancel := waitContext(ctx)
	defer cancel()
	select {
	case info := <-session.ResultCh:
		return resultFromInfo(session.ID, info)
	case <-waitCtx.Done():
		result := &Result{Text: "USSD 响应超时", Status: 5, SessionID: session.ID, DCS: defaultDCS}
		return result, fmt.Errorf("USSI 等待入站响应超时: %w", waitCtx.Err())
	}
}

func resultFromInfo(sessionID string, info InfoResult) (*Result, error) {
	if info.Err != nil {
		result := &Result{
			Text: info.Err.Error(), Status: 2, SessionID: sessionID,
			RawXML: info.RawXML, DCS: defaultDCS,
		}
		return result, info.Err
	}
	result := &Result{Text: info.Text, RawXML: info.RawXML, DCS: defaultDCS}
	if LooksLikeMenu(info.Text) {
		result.Status = 1
		result.SessionID = sessionID
	}
	return result, nil
}

func responseCarriesUSSD(response *sip.Response) bool {
	if response == nil || len(response.Body()) == 0 {
		return false
	}
	return len(ExtractFromMultipart(response.Body())) > 0 ||
		IsContentType(responseHeader(response, "Content-Type"))
}

func (s *Service) finishResult(session *Session, result *Result, err error) (*Result, error) {
	if err != nil || result == nil || result.Status != 1 {
		s.closeSession(session)
	}
	return result, err
}

func waitContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx = operationContext(ctx)
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, ussiTransactionTimeout)
}

func requestHeader(request *sip.Request, name string) string {
	if request == nil {
		return ""
	}
	header := request.GetHeader(name)
	if header == nil {
		return ""
	}
	return strings.TrimSpace(header.Value())
}

func responseHeader(response *sip.Response, name string) string {
	if response == nil {
		return ""
	}
	header := response.GetHeader(name)
	if header == nil {
		return ""
	}
	return strings.TrimSpace(header.Value())
}
