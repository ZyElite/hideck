package notify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultWeComQRGenerateURL = "https://work.weixin.qq.com/ai/qc/generate"
	defaultWeComQRQueryURL    = "https://work.weixin.qq.com/ai/qc/query_result"
	defaultWeComQRPageURL     = "https://work.weixin.qq.com/ai/qc/gen?source=hermes&scode="
	weComQRSessionTTL         = 5 * time.Minute
	weComQRResponseLimit      = 1 << 20
)

type WeComQRStatus string

const (
	WeComQRWait      WeComQRStatus = "wait"
	WeComQRScanned   WeComQRStatus = "scaned"
	WeComQRConfirmed WeComQRStatus = "confirmed"
	WeComQRExpired   WeComQRStatus = "expired"
	WeComQRError     WeComQRStatus = "error"
)

var ErrWeComQRSessionNotFound = errors.New("企业微信二维码会话不存在")

type WeComQRCredentials struct {
	BotID  string
	Secret string
}

type WeComQRView struct {
	SessionID    string
	QRURL        string
	OpenURL      string
	ExpiresAt    time.Time
	Status       WeComQRStatus
	Error        string
	Credentials  WeComQRCredentials
	Applied      bool
	ApplyWarning string
}

type WeComQROptions struct {
	HTTPClient  *http.Client
	GenerateURL string
	QueryURL    string
	PageURL     string
	Now         func() time.Time
}

type WeComQRService struct {
	client      *http.Client
	generateURL string
	queryURL    string
	pageURL     string
	now         func() time.Time
	mu          sync.RWMutex
	sessions    map[string]*weComQRSession
}

type weComQRSession struct {
	mu           sync.Mutex
	cleanup      *time.Timer
	cleanupGen   uint64
	id           string
	scode        string
	qrURL        string
	openURL      string
	expiresAt    time.Time
	status       WeComQRStatus
	errorText    string
	credentials  WeComQRCredentials
	applied      bool
	applyWarning string
}

func NewWeComQRService(options WeComQROptions) *WeComQRService {
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &WeComQRService{
		client: client, generateURL: valueOrDefault(options.GenerateURL, defaultWeComQRGenerateURL),
		queryURL: valueOrDefault(options.QueryURL, defaultWeComQRQueryURL),
		pageURL:  valueOrDefault(options.PageURL, defaultWeComQRPageURL),
		now:      now, sessions: make(map[string]*weComQRSession),
	}
}

func (s *WeComQRService) Start(ctx context.Context) (WeComQRView, error) {
	endpoint, err := appendWeComQRQuery(s.generateURL, "source", "hermes")
	if err != nil {
		return WeComQRView{}, err
	}
	var response struct {
		Data struct {
			SCode   string `json:"scode"`
			AuthURL string `json:"auth_url"`
		} `json:"data"`
	}
	if err := s.getJSON(ctx, endpoint, &response); err != nil {
		return WeComQRView{}, fmt.Errorf("获取企业微信二维码失败: %w", err)
	}
	scode := strings.TrimSpace(response.Data.SCode)
	qrURL, err := validateWeComQRDisplayURL(response.Data.AuthURL)
	if err != nil || scode == "" {
		return WeComQRView{}, errors.New("企业微信扫码服务返回的数据不完整，请手工填写 Bot ID 与 Secret")
	}
	sessionID, err := randomHex(16)
	if err != nil {
		return WeComQRView{}, fmt.Errorf("生成企业微信二维码会话 ID 失败: %w", err)
	}
	session := &weComQRSession{
		id: sessionID, scode: scode, qrURL: qrURL,
		openURL: s.pageURL + url.QueryEscape(scode), expiresAt: s.now().Add(weComQRSessionTTL), status: WeComQRWait,
	}
	s.mu.Lock()
	s.sessions[sessionID] = session
	s.mu.Unlock()
	session.mu.Lock()
	s.scheduleCleanupLocked(session)
	session.mu.Unlock()
	return session.view(), nil
}

func (s *WeComQRService) Status(ctx context.Context, sessionID string) (WeComQRView, error) {
	session, err := s.session(sessionID)
	if err != nil {
		return WeComQRView{}, err
	}
	session.mu.Lock()
	if session.terminal() {
		view := session.viewLocked()
		remove := session.status != WeComQRConfirmed || session.applied || strings.TrimSpace(session.applyWarning) != ""
		session.mu.Unlock()
		if remove {
			s.removeSession(sessionID, session)
		}
		return view, nil
	}
	if !s.now().Before(session.expiresAt) {
		session.status = WeComQRExpired
		return s.finishStatus(sessionID, session)
	}
	if err := s.pollLocked(ctx, session); err != nil {
		session.status = WeComQRError
		session.errorText = err.Error()
	}
	if session.status == WeComQRExpired || session.status == WeComQRError {
		return s.finishStatus(sessionID, session)
	}
	view := session.viewLocked()
	session.mu.Unlock()
	return view, nil
}

func (s *WeComQRService) Cancel(sessionID string) error {
	session, err := s.session(sessionID)
	if err != nil {
		return err
	}
	s.removeSession(sessionID, session)
	return nil
}

func (s *WeComQRService) MarkApplied(sessionID, warning string) error {
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

func (s *WeComQRService) finishStatus(sessionID string, session *weComQRSession) (WeComQRView, error) {
	view := session.viewLocked()
	session.mu.Unlock()
	s.removeSession(sessionID, session)
	return view, nil
}

func (s *WeComQRService) removeSession(sessionID string, session *weComQRSession) {
	session.mu.Lock()
	defer session.mu.Unlock()
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
	session.cleanupGen++
	if session.cleanup != nil {
		session.cleanup.Stop()
		session.cleanup = nil
	}
	session.clearSensitiveLocked()
}

func (s *WeComQRService) scheduleCleanupLocked(session *weComQRSession) {
	session.cleanupGen++
	generation := session.cleanupGen
	if session.cleanup != nil {
		session.cleanup.Stop()
	}
	session.cleanup = time.AfterFunc(weComQRSessionTTL+qrSessionCleanupGrace, func() {
		s.cleanupSession(session, generation)
	})
}

func (s *WeComQRService) cleanupSession(session *weComQRSession, generation uint64) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.cleanupGen != generation {
		return
	}
	s.mu.Lock()
	if s.sessions[session.id] == session {
		delete(s.sessions, session.id)
	}
	s.mu.Unlock()
	session.cleanup = nil
	session.clearSensitiveLocked()
}

func (s *WeComQRService) pollLocked(ctx context.Context, session *weComQRSession) error {
	endpoint, err := appendWeComQRQuery(s.queryURL, "scode", session.scode)
	if err != nil {
		return err
	}
	var response struct {
		Data struct {
			Status  string `json:"status"`
			BotInfo struct {
				BotID  string `json:"botid"`
				BotID2 string `json:"bot_id"`
				Secret string `json:"secret"`
			} `json:"bot_info"`
		} `json:"data"`
	}
	if err := s.getJSON(ctx, endpoint, &response); err != nil {
		return fmt.Errorf("查询企业微信扫码状态失败: %w", err)
	}
	return applyWeComQRStatus(session, response.Data.Status, response.Data.BotInfo.BotID, response.Data.BotInfo.BotID2, response.Data.BotInfo.Secret)
}

func applyWeComQRStatus(session *weComQRSession, rawStatus, botID, alternateBotID, secret string) error {
	switch strings.ToLower(strings.TrimSpace(rawStatus)) {
	case "", "wait", "waiting", "pending", "init", "created", "new", "ready":
		session.status = WeComQRWait
	case "scaned", "scanned":
		session.status = WeComQRScanned
	case "expired", "timeout":
		session.status = WeComQRExpired
	case "failed", "error":
		return errors.New("企业微信扫码服务返回失败，请重试或手工填写 Bot ID 与 Secret")
	case "success":
		if strings.TrimSpace(botID) == "" {
			botID = alternateBotID
		}
		if strings.TrimSpace(botID) == "" || strings.TrimSpace(secret) == "" {
			return errors.New("企业微信扫码成功但未返回完整凭证，请手工填写 Bot ID 与 Secret")
		}
		session.credentials = WeComQRCredentials{BotID: strings.TrimSpace(botID), Secret: strings.TrimSpace(secret)}
		session.status = WeComQRConfirmed
	default:
		return fmt.Errorf("企业微信扫码服务返回未知状态: %q", rawStatus)
	}
	return nil
}

func (s *WeComQRService) session(sessionID string) (*weComQRSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, exists := s.sessions[strings.TrimSpace(sessionID)]
	if !exists {
		return nil, ErrWeComQRSessionNotFound
	}
	return session, nil
}

func (s *weComQRSession) terminal() bool {
	return s.status == WeComQRConfirmed || s.status == WeComQRExpired || s.status == WeComQRError
}

func (s *weComQRSession) view() WeComQRView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewLocked()
}

func (s *weComQRSession) viewLocked() WeComQRView {
	return WeComQRView{
		SessionID: s.id, QRURL: s.qrURL, OpenURL: s.openURL, ExpiresAt: s.expiresAt,
		Status: s.status, Error: s.errorText, Credentials: s.credentials,
		Applied: s.applied, ApplyWarning: s.applyWarning,
	}
}

func (s *weComQRSession) clearSensitiveLocked() {
	s.scode = ""
	s.credentials.Secret = ""
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
