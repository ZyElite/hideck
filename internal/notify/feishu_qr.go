package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultFeishuAccountsURL = "https://accounts.feishu.cn"
	defaultLarkAccountsURL   = "https://accounts.larksuite.com"
	feishuQRRegistrationPath = "/oauth/v1/app/registration"
	feishuQRSessionTTL       = 8 * time.Minute
	feishuQRResponseLimit    = 1 << 20
)

type FeishuQRStatus string

const (
	FeishuQRWait      FeishuQRStatus = "wait"
	FeishuQRScanned   FeishuQRStatus = "scaned"
	FeishuQRConfirmed FeishuQRStatus = "confirmed"
	FeishuQRExpired   FeishuQRStatus = "expired"
	FeishuQRError     FeishuQRStatus = "error"
)

var ErrFeishuQRSessionNotFound = errors.New("飞书二维码会话不存在")

type FeishuQRCredentials struct {
	AppID     string
	AppSecret string
	Domain    string
	OpenID    string
}

type FeishuQRView struct {
	SessionID    string
	QRURL        string
	OpenURL      string
	ExpiresAt    time.Time
	Status       FeishuQRStatus
	Error        string
	Credentials  FeishuQRCredentials
	Applied      bool
	ApplyWarning string
}

type FeishuQROptions struct {
	HTTPClient  *http.Client
	AccountsURL string
	LarkURL     string
	Now         func() time.Time
}

type FeishuQRService struct {
	client      *http.Client
	accountsURL string
	larkURL     string
	now         func() time.Time
	mu          sync.RWMutex
	sessions    map[string]*feishuQRSession
}

type feishuQRSession struct {
	mu           sync.Mutex
	cleanup      *time.Timer
	cleanupGen   uint64
	id           string
	deviceCode   string
	domain       string
	qrURL        string
	expiresAt    time.Time
	status       FeishuQRStatus
	errorText    string
	credentials  FeishuQRCredentials
	applied      bool
	applyWarning string
}

type feishuQRRegistrationResponse struct {
	DeviceCode              string           `json:"device_code"`
	VerificationURIComplete string           `json:"verification_uri_complete"`
	UserCode                string           `json:"user_code"`
	Interval                int              `json:"interval"`
	ExpireIn                int              `json:"expire_in"`
	SupportedAuthMethods    []string         `json:"supported_auth_methods"`
	ClientID                string           `json:"client_id"`
	ClientSecret            string           `json:"client_secret"`
	Error                   string           `json:"error"`
	UserInfo                feishuQRUserInfo `json:"user_info"`
}

type feishuQRUserInfo struct {
	OpenID      string `json:"open_id"`
	TenantBrand string `json:"tenant_brand"`
}

func NewFeishuQRService(options FeishuQROptions) *FeishuQRService {
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &FeishuQRService{
		client: client, accountsURL: valueOrDefault(options.AccountsURL, defaultFeishuAccountsURL),
		larkURL: valueOrDefault(options.LarkURL, defaultLarkAccountsURL),
		now:     now, sessions: make(map[string]*feishuQRSession),
	}
}

func (s *FeishuQRService) Start(ctx context.Context) (FeishuQRView, error) {
	if err := s.initRegistration(ctx, s.accountsURL); err != nil {
		return FeishuQRView{}, err
	}
	begin, err := s.beginRegistration(ctx, s.accountsURL)
	if err != nil {
		return FeishuQRView{}, err
	}
	qrURL := annotateFeishuQRURL(begin.VerificationURIComplete)
	if strings.TrimSpace(begin.DeviceCode) == "" || qrURL == "" {
		return FeishuQRView{}, errors.New("飞书扫码服务返回的数据不完整，请手工填写 App ID 与 App Secret")
	}
	sessionID, err := randomHex(16)
	if err != nil {
		return FeishuQRView{}, fmt.Errorf("生成飞书二维码会话 ID 失败: %w", err)
	}
	ttl := feishuQRSessionTTL
	if begin.ExpireIn > 0 {
		if expire := time.Duration(begin.ExpireIn) * time.Second; expire < ttl {
			ttl = expire
		}
	}
	session := &feishuQRSession{
		id: sessionID, deviceCode: strings.TrimSpace(begin.DeviceCode),
		domain: "feishu", qrURL: qrURL, expiresAt: s.now().Add(ttl), status: FeishuQRWait,
	}
	s.mu.Lock()
	s.sessions[sessionID] = session
	s.mu.Unlock()
	session.mu.Lock()
	s.scheduleCleanupLocked(session)
	session.mu.Unlock()
	return session.view(), nil
}

func (s *FeishuQRService) Status(ctx context.Context, sessionID string) (FeishuQRView, error) {
	session, err := s.session(sessionID)
	if err != nil {
		return FeishuQRView{}, err
	}
	session.mu.Lock()
	if session.terminal() {
		view := session.viewLocked()
		remove := session.status != FeishuQRConfirmed || session.applied || strings.TrimSpace(session.applyWarning) != ""
		session.mu.Unlock()
		if remove {
			s.removeSession(sessionID, session)
		}
		return view, nil
	}
	if !s.now().Before(session.expiresAt) {
		session.status = FeishuQRExpired
		return s.finishStatus(sessionID, session)
	}
	if err := s.pollLocked(ctx, session); err != nil {
		session.status = FeishuQRError
		session.errorText = err.Error()
	}
	if session.status == FeishuQRExpired || session.status == FeishuQRError {
		return s.finishStatus(sessionID, session)
	}
	view := session.viewLocked()
	session.mu.Unlock()
	return view, nil
}

func (s *FeishuQRService) Cancel(sessionID string) error {
	session, err := s.session(sessionID)
	if err != nil {
		return err
	}
	s.removeSession(sessionID, session)
	return nil
}

func (s *FeishuQRService) MarkApplied(sessionID, warning string) error {
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

func (s *FeishuQRService) finishStatus(sessionID string, session *feishuQRSession) (FeishuQRView, error) {
	view := session.viewLocked()
	session.mu.Unlock()
	s.removeSession(sessionID, session)
	return view, nil
}

func (s *FeishuQRService) removeSession(sessionID string, session *feishuQRSession) {
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

func (s *FeishuQRService) scheduleCleanupLocked(session *feishuQRSession) {
	session.cleanupGen++
	generation := session.cleanupGen
	if session.cleanup != nil {
		session.cleanup.Stop()
	}
	session.cleanup = time.AfterFunc(feishuQRSessionTTL+qrSessionCleanupGrace, func() {
		s.cleanupSession(session, generation)
	})
}

func (s *FeishuQRService) cleanupSession(session *feishuQRSession, generation uint64) {
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

func (s *FeishuQRService) pollLocked(ctx context.Context, session *feishuQRSession) error {
	baseURL := s.accountsURL
	if session.domain == "lark" {
		baseURL = s.larkURL
	}
	response, err := s.postRegistration(ctx, baseURL, url.Values{
		"action":      {"poll"},
		"device_code": {session.deviceCode},
		"tp":          {"ob_app"},
	})
	if err != nil {
		return fmt.Errorf("查询飞书扫码状态失败: %w", err)
	}
	if strings.EqualFold(strings.TrimSpace(response.UserInfo.TenantBrand), "lark") {
		session.domain = "lark"
	}
	return applyFeishuQRPoll(session, response)
}

func applyFeishuQRPoll(session *feishuQRSession, response feishuQRRegistrationResponse) error {
	if strings.TrimSpace(response.ClientID) != "" && strings.TrimSpace(response.ClientSecret) != "" {
		session.credentials = FeishuQRCredentials{
			AppID: strings.TrimSpace(response.ClientID), AppSecret: strings.TrimSpace(response.ClientSecret),
			Domain: session.domain, OpenID: strings.TrimSpace(response.UserInfo.OpenID),
		}
		session.status = FeishuQRConfirmed
		return nil
	}
	switch strings.TrimSpace(response.Error) {
	case "", "authorization_pending", "slow_down":
		session.status = FeishuQRWait
		return nil
	case "access_denied":
		return errors.New("飞书扫码已被拒绝，请重试或手工填写 App ID 与 App Secret")
	case "expired_token":
		session.status = FeishuQRExpired
		return nil
	default:
		return fmt.Errorf("飞书扫码服务返回未知状态: %q", response.Error)
	}
}

func (s *FeishuQRService) initRegistration(ctx context.Context, baseURL string) error {
	response, err := s.postRegistration(ctx, baseURL, url.Values{"action": {"init"}})
	if err != nil {
		return fmt.Errorf("初始化飞书扫码失败: %w", err)
	}
	for _, method := range response.SupportedAuthMethods {
		if strings.TrimSpace(method) == "client_secret" {
			return nil
		}
	}
	if len(response.SupportedAuthMethods) == 0 {
		return nil
	}
	return fmt.Errorf("飞书扫码环境不支持自动创建应用，请手工填写 App ID 与 App Secret")
}

func (s *FeishuQRService) beginRegistration(ctx context.Context, baseURL string) (feishuQRRegistrationResponse, error) {
	response, err := s.postRegistration(ctx, baseURL, url.Values{
		"action":            {"begin"},
		"archetype":         {"PersonalAgent"},
		"auth_method":       {"client_secret"},
		"request_user_info": {"open_id"},
	})
	if err != nil {
		return feishuQRRegistrationResponse{}, fmt.Errorf("获取飞书二维码失败: %w", err)
	}
	return response, nil
}

func (s *FeishuQRService) postRegistration(ctx context.Context, baseURL string, form url.Values) (feishuQRRegistrationResponse, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(baseURL), "/") + feishuQRRegistrationPath
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return feishuQRRegistrationResponse{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.client.Do(request)
	if err != nil {
		return feishuQRRegistrationResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, feishuQRResponseLimit))
	if err != nil {
		return feishuQRRegistrationResponse{}, err
	}
	var parsed feishuQRRegistrationResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		if response.StatusCode >= 400 {
			return feishuQRRegistrationResponse{}, fmt.Errorf("飞书扫码服务返回 %d", response.StatusCode)
		}
		return feishuQRRegistrationResponse{}, fmt.Errorf("解析飞书扫码响应失败: %w", err)
	}
	return parsed, nil
}

func annotateFeishuQRURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value
	}
	query := parsed.Query()
	if query.Get("from") == "" {
		query.Set("from", "hideck")
	}
	if query.Get("tp") == "" {
		query.Set("tp", "hideck")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (s *FeishuQRService) session(sessionID string) (*feishuQRSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[strings.TrimSpace(sessionID)]
	if session == nil {
		return nil, ErrFeishuQRSessionNotFound
	}
	return session, nil
}

func (s *feishuQRSession) terminal() bool {
	return s.status == FeishuQRConfirmed || s.status == FeishuQRExpired || s.status == FeishuQRError
}

func (s *feishuQRSession) view() FeishuQRView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewLocked()
}

func (s *feishuQRSession) viewLocked() FeishuQRView {
	return FeishuQRView{
		SessionID: s.id, QRURL: s.qrURL, OpenURL: s.qrURL, ExpiresAt: s.expiresAt,
		Status: s.status, Error: s.errorText, Credentials: s.credentials,
		Applied: s.applied, ApplyWarning: s.applyWarning,
	}
}

func (s *feishuQRSession) clearSensitiveLocked() {
	s.deviceCode = ""
	s.credentials.AppSecret = ""
}
