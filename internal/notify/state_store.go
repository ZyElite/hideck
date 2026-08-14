package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const runtimeStateVersion = 1

var ErrRuntimeStateStoreUnavailable = errors.New("通知渠道运行状态存储未配置")

type WeixinRuntimeState struct {
	AccountID     string            `json:"account_id,omitempty"`
	Token         string            `json:"token,omitempty"`
	BaseURL       string            `json:"base_url,omitempty"`
	UserID        string            `json:"user_id,omitempty"`
	SyncBuffer    string            `json:"sync_buffer,omitempty"`
	ContextTokens map[string]string `json:"context_tokens,omitempty"`
	AllowedUsers  []string          `json:"allowed_users,omitempty"`
	DefaultTarget string            `json:"default_target,omitempty"`
}

type WeComBotRuntimeState struct {
	AllowedUsers  []string `json:"allowed_users,omitempty"`
	DefaultTarget string   `json:"default_target,omitempty"`
}

type QQRuntimeState struct {
	AdminOpenID   string   `json:"admin_openid,omitempty"`
	AllowedDirect []string `json:"allowed_direct,omitempty"`
	DefaultTarget string   `json:"default_target,omitempty"`
}

type RuntimeState struct {
	Version  int                  `json:"version"`
	Weixin   WeixinRuntimeState   `json:"weixin"`
	WeComBot WeComBotRuntimeState `json:"wecom_bot"`
	QQ       QQRuntimeState       `json:"qq"`
}

type RuntimeStateStore interface {
	Load() (RuntimeState, error)
	Save(state RuntimeState) error
	Update(update func(*RuntimeState) error) error
}

type FileRuntimeStateStore struct {
	path string
	mu   sync.Mutex
}

func NewFileRuntimeStateStore(path string) *FileRuntimeStateStore {
	return &FileRuntimeStateStore{path: filepath.Clean(path)}
}

func (s *FileRuntimeStateStore) Load() (RuntimeState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *FileRuntimeStateStore) loadLocked() (RuntimeState, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return newRuntimeState(), nil
	}
	if err != nil {
		return RuntimeState{}, fmt.Errorf("读取通知渠道运行状态失败: %w", err)
	}
	var state RuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return RuntimeState{}, fmt.Errorf("解析通知渠道运行状态失败: %w", err)
	}
	if state.Version != runtimeStateVersion {
		return RuntimeState{}, fmt.Errorf("不支持的通知渠道运行状态版本: %d", state.Version)
	}
	return cloneRuntimeState(state), nil
}

func (s *FileRuntimeStateStore) Save(state RuntimeState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(state)
}

func (s *FileRuntimeStateStore) Update(update func(*RuntimeState) error) error {
	if update == nil {
		return errors.New("通知渠道运行状态更新函数不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadLocked()
	if err != nil {
		return err
	}
	if err := update(&state); err != nil {
		return err
	}
	return s.saveLocked(state)
}

func (s *FileRuntimeStateStore) saveLocked(state RuntimeState) error {
	state = cloneRuntimeState(state)
	state.Version = runtimeStateVersion
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化通知渠道运行状态失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("创建通知渠道状态目录失败: %w", err)
	}
	if err := writePrivateFileAtomically(s.path, append(data, '\n')); err != nil {
		return fmt.Errorf("保存通知渠道运行状态失败: %w", err)
	}
	return nil
}

func newRuntimeState() RuntimeState {
	return RuntimeState{Version: runtimeStateVersion}
}

func cloneRuntimeState(state RuntimeState) RuntimeState {
	state.Weixin.ContextTokens = cloneStringMap(state.Weixin.ContextTokens)
	state.Weixin.AllowedUsers = append([]string(nil), state.Weixin.AllowedUsers...)
	state.WeComBot.AllowedUsers = append([]string(nil), state.WeComBot.AllowedUsers...)
	state.QQ.AllowedDirect = append([]string(nil), state.QQ.AllowedDirect...)
	return state
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func writePrivateFileAtomically(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".notification-state-*")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
