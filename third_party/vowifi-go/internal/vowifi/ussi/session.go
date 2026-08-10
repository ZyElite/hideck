package ussi

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

func contextFromEndpoint(endpoint imsendpoint.ClientDialogEndpoint) (Context, error) {
	if endpoint == nil {
		return Context{}, errors.New("USSI endpoint 为空")
	}
	snapshot := endpoint.Snapshot()
	localIP, localPortC := splitLocalAddr(snapshot.LocalAddr, snapshot.LocalPortC)
	aor := strings.TrimSpace(snapshot.IMPU)
	if aor == "" {
		return Context{}, errors.New("USSI AOR 为空")
	}
	domain := domainFromAOR(aor)
	if domain == "" {
		domain = strings.TrimSpace(snapshot.Realm)
	}
	return Context{
		LocalIP: localIP, LocalPortC: localPortC, LocalPortS: snapshot.LocalPortS,
		Transport: snapshot.Transport, Domain: domain, Realm: snapshot.Realm, AOR: aor,
		RouteHeader: snapshot.ServiceRoute, ServiceRoute: snapshot.ServiceRoute,
		SecVerify: snapshot.SecVerify, Mode: snapshot.EffectiveSecMode,
		PANI: snapshot.PAccessNetworkInfo, UserAgent: snapshot.UserAgent,
		ContactID: snapshot.ContactID,
	}, nil
}

func domainFromAOR(aor string) string {
	aor = strings.Trim(strings.TrimSpace(aor), "<>")
	index := strings.LastIndexByte(aor, '@')
	if index < 0 || index+1 >= len(aor) {
		return ""
	}
	domain := strings.Trim(aor[index+1:], "<>")
	if semicolon := strings.IndexByte(domain, ';'); semicolon >= 0 {
		domain = domain[:semicolon]
	}
	return strings.TrimSpace(domain)
}

func (s *Service) activeSession() *Session {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil || !s.session.IsActive() {
		return nil
	}
	return s.session
}

func (s *Service) sessionFor(sessionID string) (*Session, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("USSD session ID 为空")
	}
	session := s.activeSession()
	if session == nil || session.ID != sessionID {
		return nil, errors.New("USSD session 不存在或已结束")
	}
	return session, nil
}

func (s *Service) matchInboundSession(request *sip.Request) (*Session, error) {
	if request == nil || request.CallID() == nil {
		return nil, errors.New("缺少 Call-ID")
	}
	session := s.activeSession()
	if session == nil {
		return nil, errors.New("没有活动 USSD 会话")
	}
	if strings.TrimSpace(request.CallID().Value()) != session.CallID {
		return nil, errors.New("USSD Call-ID 不匹配")
	}
	return session, nil
}

func sessionDialog(session *Session) (imsendpoint.DialogHandle, Context, bool) {
	if session == nil {
		return nil, Context{}, false
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.State != sessionActive || session.dialogHandle == nil {
		return nil, Context{}, false
	}
	return session.dialogHandle, session.dialogContext, true
}

func touchSession(session *Session) {
	if session == nil {
		return
	}
	session.mu.Lock()
	session.LastAt = time.Now()
	session.mu.Unlock()
}

func snapshotSessionDialog(session *Session) (imsendpoint.DialogHandle, bool) {
	if session == nil {
		return nil, false
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.State != sessionActive || session.dialogHandle == nil {
		return nil, false
	}
	return session.dialogHandle, true
}

func (s *Service) setSession(session *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil && s.session.IsActive() {
		return
	}
	s.session = session
}

func (s *Service) ownsSession(session *Session) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session == session
}

func (s *Service) clearSession(sessionID string) *Session {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	session := s.session
	sessionID = strings.TrimSpace(sessionID)
	if session == nil || (sessionID != "" && session.ID != sessionID) {
		s.mu.Unlock()
		return nil
	}
	s.session = nil
	s.mu.Unlock()
	session.Terminate()
	return session
}

func (s *Service) closeSession(session *Session) {
	if session == nil {
		return
	}
	dialog, ok := snapshotSessionDialog(session)
	s.clearSession(session.ID)
	if !ok || s.endpoint == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), ussiTransactionTimeout)
	defer cancel()
	_ = s.endpoint.CloseDialog(ctx, s.deviceID, dialog)
}

// Stop releases the active dialog and wakes any blocked operation.
func (s *Service) Stop() {
	session := s.activeSession()
	if session == nil {
		return
	}
	deliverInfoResult(session.ResultCh, InfoResult{Err: errors.New("USSI service stopped")})
	s.closeSession(session)
}
