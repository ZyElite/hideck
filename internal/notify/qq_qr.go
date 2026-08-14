package notify

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultQQQRBaseURL = "https://q.qq.com"
	qqQRSessionTTL     = 10 * time.Minute
	qqQRMaxRefreshes   = 3
)

type QQQRStatus string

const (
	QQQRWait      QQQRStatus = "wait"
	QQQRConfirmed QQQRStatus = "confirmed"
	QQQRExpired   QQQRStatus = "expired"
	QQQRError     QQQRStatus = "error"
)

var ErrQQQRSessionNotFound = errors.New("QQ 二维码会话不存在")

type QQQRCredentials struct {
	AppID        string
	ClientSecret string
	UserOpenID   string
}

type QQQRView struct {
	SessionID    string
	TaskID       string
	QRURL        string
	ExpiresAt    time.Time
	Status       QQQRStatus
	Error        string
	Credentials  QQQRCredentials
	Applied      bool
	ApplyWarning string
}

type QQQROptions struct {
	HTTPClient *http.Client
	BaseURL    string
	QRBaseURL  string
	Now        func() time.Time
}

type QQQRService struct {
	client    *qqQRHTTPClient
	qrBaseURL string
	now       func() time.Time
	mu        sync.RWMutex
	sessions  map[string]*qqQRSession
}

type qqQRSession struct {
	mu           sync.Mutex
	cleanup      *time.Timer
	cleanupGen   uint64
	id           string
	taskID       string
	qrURL        string
	key          []byte
	expiresAt    time.Time
	status       QQQRStatus
	refreshCount int
	errorText    string
	credentials  QQQRCredentials
	applied      bool
	applyWarning string
}

func NewQQQRService(options QQQROptions) *QQQRService {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultQQQRBaseURL
	}
	qrBaseURL := strings.TrimRight(strings.TrimSpace(options.QRBaseURL), "/")
	if qrBaseURL == "" {
		qrBaseURL = defaultQQQRBaseURL
	}
	return &QQQRService{
		client: newQQQRHTTPClient(baseURL, options.HTTPClient), qrBaseURL: qrBaseURL,
		now: now, sessions: make(map[string]*qqQRSession),
	}
}

func (s *QQQRService) Start(ctx context.Context) (QQQRView, error) {
	taskID, key, err := s.createTask(ctx)
	if err != nil {
		return QQQRView{}, err
	}
	sessionID, err := randomHex(16)
	if err != nil {
		return QQQRView{}, fmt.Errorf("生成 QQ 二维码会话 ID 失败: %w", err)
	}
	session := &qqQRSession{
		id: sessionID, taskID: taskID, key: key, qrURL: buildQQQRURL(s.qrBaseURL, taskID),
		expiresAt: s.now().Add(qqQRSessionTTL), status: QQQRWait,
	}
	s.mu.Lock()
	s.sessions[sessionID] = session
	s.mu.Unlock()
	session.mu.Lock()
	s.scheduleCleanupLocked(session)
	session.mu.Unlock()
	return session.view(), nil
}

func (s *QQQRService) Status(ctx context.Context, sessionID string) (QQQRView, error) {
	session, err := s.session(sessionID)
	if err != nil {
		return QQQRView{}, err
	}
	session.mu.Lock()
	if session.terminal() {
		view := session.viewLocked()
		session.mu.Unlock()
		s.removeSession(sessionID, session)
		return view, nil
	}
	if !s.now().Before(session.expiresAt) {
		session.status = QQQRExpired
		return s.finishStatus(sessionID, session)
	}
	result, err := s.client.poll(ctx, session.taskID)
	if err != nil {
		view := session.viewLocked()
		session.mu.Unlock()
		return view, err
	}
	if err := s.applyPollResult(ctx, session, result); err != nil {
		session.status = QQQRError
		session.errorText = err.Error()
	}
	if session.status == QQQRExpired || session.status == QQQRError {
		return s.finishStatus(sessionID, session)
	}
	view := session.viewLocked()
	session.mu.Unlock()
	return view, nil
}

func (s *QQQRService) Cancel(sessionID string) error {
	session, err := s.session(sessionID)
	if err != nil {
		return err
	}
	s.removeSession(sessionID, session)
	return nil
}

func (s *QQQRService) MarkApplied(sessionID, warning string) error {
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

func (s *QQQRService) finishStatus(sessionID string, session *qqQRSession) (QQQRView, error) {
	view := session.viewLocked()
	session.mu.Unlock()
	s.removeSession(sessionID, session)
	return view, nil
}

func (s *QQQRService) removeSession(sessionID string, session *qqQRSession) {
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

func (s *QQQRService) scheduleCleanupLocked(session *qqQRSession) {
	session.cleanupGen++
	generation := session.cleanupGen
	if session.cleanup != nil {
		session.cleanup.Stop()
	}
	session.cleanup = time.AfterFunc(qqQRSessionTTL+qrSessionCleanupGrace, func() {
		s.cleanupSession(session, generation)
	})
}

func (s *QQQRService) cleanupSession(session *qqQRSession, generation uint64) {
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

func (s *QQQRService) createTask(ctx context.Context) (string, []byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", nil, fmt.Errorf("生成 QQ 扫码 AES 密钥失败: %w", err)
	}
	taskID, err := s.client.create(ctx, base64.StdEncoding.EncodeToString(key))
	if err != nil {
		clear(key)
		return "", nil, err
	}
	return taskID, key, nil
}

func (s *QQQRService) applyPollResult(ctx context.Context, session *qqQRSession, result qqQRPollResult) error {
	switch result.Status {
	case 0, 1:
		session.status = QQQRWait
	case 2:
		secret, err := decryptQQQRSecret(result.EncryptedSecret, session.key)
		if err != nil {
			return err
		}
		if strings.TrimSpace(result.AppID) == "" || strings.TrimSpace(result.UserOpenID) == "" {
			return errors.New("QQ 扫码完成但未返回 App ID 或扫码用户 OpenID")
		}
		session.credentials = QQQRCredentials{
			AppID: strings.TrimSpace(result.AppID), ClientSecret: secret,
			UserOpenID: strings.TrimSpace(result.UserOpenID),
		}
		session.status = QQQRConfirmed
	case 3:
		return s.refreshLocked(ctx, session)
	default:
		return fmt.Errorf("QQ 扫码服务返回未知状态: %d", result.Status)
	}
	return nil
}

func (s *QQQRService) refreshLocked(ctx context.Context, session *qqQRSession) error {
	if session.refreshCount >= qqQRMaxRefreshes {
		session.status = QQQRExpired
		return nil
	}
	taskID, key, err := s.createTask(ctx)
	if err != nil {
		return fmt.Errorf("刷新 QQ 二维码失败: %w", err)
	}
	clear(session.key)
	session.taskID, session.key = taskID, key
	session.qrURL = buildQQQRURL(s.qrBaseURL, taskID)
	session.expiresAt = s.now().Add(qqQRSessionTTL)
	session.refreshCount++
	session.status = QQQRWait
	s.scheduleCleanupLocked(session)
	return nil
}

func (s *QQQRService) session(sessionID string) (*qqQRSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, exists := s.sessions[strings.TrimSpace(sessionID)]
	if !exists {
		return nil, ErrQQQRSessionNotFound
	}
	return session, nil
}

func (s *qqQRSession) terminal() bool {
	return s.status == QQQRConfirmed || s.status == QQQRExpired || s.status == QQQRError
}

func (s *qqQRSession) view() QQQRView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewLocked()
}

func (s *qqQRSession) viewLocked() QQQRView {
	return QQQRView{
		SessionID: s.id, TaskID: s.taskID, QRURL: s.qrURL, ExpiresAt: s.expiresAt,
		Status: s.status, Error: s.errorText, Credentials: s.credentials,
		Applied: s.applied, ApplyWarning: s.applyWarning,
	}
}

func (s *qqQRSession) clearSensitiveLocked() {
	clear(s.key)
	s.key = nil
	s.credentials.ClientSecret = ""
}

func buildQQQRURL(baseURL, taskID string) string {
	return strings.TrimRight(baseURL, "/") + "/qqbot/openclaw/connect.html?task_id=" +
		url.QueryEscape(strings.TrimSpace(taskID)) + "&_wv=2&source=vohive"
}
