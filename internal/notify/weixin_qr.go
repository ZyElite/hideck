package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	weixinQRMaxRefreshes = 3
	weixinQRSessionTTL   = 8 * time.Minute
)

type WeixinQRStatus string

const (
	WeixinQRWait      WeixinQRStatus = "wait"
	WeixinQRScanned   WeixinQRStatus = "scaned"
	WeixinQRConfirmed WeixinQRStatus = "confirmed"
	WeixinQRExpired   WeixinQRStatus = "expired"
	WeixinQRError     WeixinQRStatus = "error"
)

var ErrWeixinQRSessionNotFound = errors.New("微信二维码会话不存在")

type WeixinQRCredentials struct {
	AccountID string
	Token     string
	BaseURL   string
	UserID    string
}

type WeixinQRView struct {
	SessionID    string
	QRToken      string
	QRURL        string
	ExpiresAt    time.Time
	Status       WeixinQRStatus
	Error        string
	Credentials  WeixinQRCredentials
	Applied      bool
	ApplyWarning string
}

type WeixinQROptions struct {
	HTTPClient *http.Client
	Now        func() time.Time
}

type WeixinQRService struct {
	client   *weixinILinkClient
	now      func() time.Time
	mu       sync.RWMutex
	sessions map[string]*weixinQRSession
}

type weixinQRSession struct {
	mu           sync.Mutex
	cleanup      *time.Timer
	id           string
	baseURL      string
	pollBaseURL  string
	qrToken      string
	qrURL        string
	expiresAt    time.Time
	status       WeixinQRStatus
	refreshCount int
	errorText    string
	credentials  WeixinQRCredentials
	applied      bool
	applyWarning string
}

func NewWeixinQRService(options WeixinQROptions) *WeixinQRService {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &WeixinQRService{
		client: newWeixinILinkClient(options.HTTPClient), now: now,
		sessions: make(map[string]*weixinQRSession),
	}
}

func (s *WeixinQRService) Start(ctx context.Context, baseURL string) (WeixinQRView, error) {
	baseURL, err := normalizeWeixinBaseURL(baseURL)
	if err != nil {
		return WeixinQRView{}, err
	}
	qrToken, qrURL, err := s.client.fetchQRCode(ctx, baseURL)
	if err != nil {
		return WeixinQRView{}, err
	}
	sessionID, err := randomHex(16)
	if err != nil {
		return WeixinQRView{}, fmt.Errorf("生成微信二维码会话 ID 失败: %w", err)
	}
	session := &weixinQRSession{
		id: sessionID, baseURL: baseURL, pollBaseURL: baseURL,
		qrToken: qrToken, qrURL: qrURL, expiresAt: s.now().Add(weixinQRSessionTTL), status: WeixinQRWait,
	}
	s.mu.Lock()
	s.sessions[sessionID] = session
	s.mu.Unlock()
	session.cleanup = time.AfterFunc(weixinQRSessionTTL+qrSessionCleanupGrace, func() {
		s.removeSession(sessionID, session)
	})
	return session.view(), nil
}

func (s *WeixinQRService) Status(ctx context.Context, sessionID string) (WeixinQRView, error) {
	session, err := s.session(sessionID)
	if err != nil {
		return WeixinQRView{}, err
	}
	session.mu.Lock()
	if session.terminal() {
		view := session.viewLocked()
		session.mu.Unlock()
		s.removeSession(sessionID, session)
		return view, nil
	}
	if !s.now().Before(session.expiresAt) {
		session.status = WeixinQRExpired
		return s.finishStatus(sessionID, session)
	}
	if err := s.pollLocked(ctx, session); err != nil {
		session.status = WeixinQRError
		session.errorText = err.Error()
	}
	if session.status == WeixinQRExpired || session.status == WeixinQRError {
		return s.finishStatus(sessionID, session)
	}
	view := session.viewLocked()
	session.mu.Unlock()
	return view, nil
}

func (s *WeixinQRService) Cancel(sessionID string) error {
	session, err := s.session(sessionID)
	if err != nil {
		return err
	}
	s.removeSession(sessionID, session)
	return nil
}

func (s *WeixinQRService) MarkApplied(sessionID, warning string) error {
	session, err := s.session(sessionID)
	if err != nil {
		return err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	session.applied = strings.TrimSpace(warning) == ""
	session.applyWarning = strings.TrimSpace(warning)
	session.clearSensitiveLocked()
	return nil
}

func (s *WeixinQRService) finishStatus(sessionID string, session *weixinQRSession) (WeixinQRView, error) {
	view := session.viewLocked()
	session.mu.Unlock()
	s.removeSession(sessionID, session)
	return view, nil
}

func (s *WeixinQRService) removeSession(sessionID string, session *weixinQRSession) {
	s.mu.Lock()
	removed := false
	if s.sessions[strings.TrimSpace(sessionID)] == session {
		delete(s.sessions, strings.TrimSpace(sessionID))
		removed = true
	}
	s.mu.Unlock()
	if !removed {
		return
	}
	session.mu.Lock()
	if session.cleanup != nil {
		session.cleanup.Stop()
		session.cleanup = nil
	}
	session.clearSensitiveLocked()
	session.mu.Unlock()
}

func (s *WeixinQRService) pollLocked(ctx context.Context, session *weixinQRSession) error {
	response, err := s.client.pollQR(ctx, session.pollBaseURL, session.qrToken)
	if err != nil {
		return err
	}
	switch response.Status {
	case "wait":
		session.status = WeixinQRWait
	case "scaned":
		session.status = WeixinQRScanned
	case "scaned_but_redirect":
		return applyWeixinQRRedirect(session, response.RedirectHost)
	case "expired":
		return s.refreshLocked(ctx, session)
	case "confirmed":
		return confirmWeixinQRSession(session, response)
	default:
		return fmt.Errorf("微信二维码返回未知状态: %q", response.Status)
	}
	return nil
}

func (s *WeixinQRService) refreshLocked(ctx context.Context, session *weixinQRSession) error {
	if session.refreshCount >= weixinQRMaxRefreshes {
		session.status = WeixinQRExpired
		return nil
	}
	qrToken, qrURL, err := s.client.fetchQRCode(ctx, session.baseURL)
	if err != nil {
		return fmt.Errorf("刷新微信二维码失败: %w", err)
	}
	session.refreshCount++
	session.qrToken, session.qrURL = qrToken, qrURL
	session.pollBaseURL = session.baseURL
	session.expiresAt = s.now().Add(weixinQRSessionTTL)
	session.status = WeixinQRWait
	return nil
}

func applyWeixinQRRedirect(session *weixinQRSession, host string) error {
	redirectURL, err := redirectWeixinBaseURL(host)
	if err != nil {
		return err
	}
	session.pollBaseURL = redirectURL
	session.status = WeixinQRScanned
	return nil
}

func confirmWeixinQRSession(session *weixinQRSession, response weixinQRPlatformStatus) error {
	if strings.TrimSpace(response.AccountID) == "" || strings.TrimSpace(response.Token) == "" {
		return errors.New("微信二维码已确认，但凭证信息不完整")
	}
	baseURL := strings.TrimSpace(response.BaseURL)
	if baseURL == "" {
		baseURL = session.baseURL
	}
	var err error
	if baseURL, err = normalizeWeixinBaseURL(baseURL); err != nil {
		return fmt.Errorf("微信凭证 base_url 无效: %w", err)
	}
	session.credentials = WeixinQRCredentials{
		AccountID: strings.TrimSpace(response.AccountID), Token: strings.TrimSpace(response.Token),
		BaseURL: baseURL, UserID: strings.TrimSpace(response.UserID),
	}
	session.status = WeixinQRConfirmed
	return nil
}

func randomHex(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (s *WeixinQRService) session(sessionID string) (*weixinQRSession, error) {
	s.mu.RLock()
	session := s.sessions[strings.TrimSpace(sessionID)]
	s.mu.RUnlock()
	if session == nil {
		return nil, ErrWeixinQRSessionNotFound
	}
	return session, nil
}

func (s *weixinQRSession) terminal() bool {
	return s.status == WeixinQRConfirmed || s.status == WeixinQRExpired || s.status == WeixinQRError
}

func (s *weixinQRSession) view() WeixinQRView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewLocked()
}

func (s *weixinQRSession) viewLocked() WeixinQRView {
	return WeixinQRView{
		SessionID: s.id, QRToken: s.qrToken, QRURL: s.qrURL, ExpiresAt: s.expiresAt,
		Status: s.status, Error: s.errorText, Credentials: s.credentials,
		Applied: s.applied, ApplyWarning: s.applyWarning,
	}
}

func (s *weixinQRSession) clearSensitiveLocked() {
	s.qrToken = ""
	s.credentials.Token = ""
}
