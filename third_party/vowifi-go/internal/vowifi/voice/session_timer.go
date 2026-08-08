package voice

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

const (
	minimumHalfSessionRefresh  = 90 * time.Second
	shortSessionRefreshLead    = 10 * time.Second
	voiceSessionRefreshTimeout = 5 * time.Second
)

func parseVoiceSessionExpires(value string) (time.Duration, bool) {
	secondsText, _, _ := strings.Cut(strings.TrimSpace(value), ";")
	if secondsText == "" {
		return 0, false
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(secondsText))
	if err != nil || seconds <= 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

func (c *Call) applyVoiceSessionExpires(value string) {
	expires, ok := parseVoiceSessionExpires(value)
	if strings.TrimSpace(value) != "" && !ok {
		logging.WarnRate("ims-voice-session-expires-invalid", "IMS 会话过期时间无效", "value", value)
		return
	}
	if !ok {
		return
	}
	c.mu.Lock()
	c.sessionExpires = expires
	c.mu.Unlock()
}

func (c *Call) voiceSessionExpires() time.Duration {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionExpires
}

func sessionRefreshDelay(expires time.Duration) time.Duration {
	half := expires / 2
	if half >= minimumHalfSessionRefresh {
		return half
	}
	if expires > shortSessionRefreshLead {
		return expires - shortSessionRefreshLead
	}
	return expires
}

// StartSessionTimer schedules the negotiated RFC 4028 session refresh.
func (c *Call) StartSessionTimer(expires time.Duration) error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	if expires > 0 {
		c.mu.Lock()
		c.sessionExpires = expires
		c.mu.Unlock()
	}
	expires = c.voiceSessionExpires()
	if expires <= 0 {
		return c.stopSessionTimer()
	}
	if c.agent == nil || c.agent.ims == nil {
		return errors.New("voice: session timer has no IMS agent")
	}
	c.scheduleSessionRefresh(sessionRefreshDelay(expires))
	return nil
}

func (c *Call) scheduleSessionRefresh(delay time.Duration) {
	c.mu.Lock()
	if c.sessionTimer != nil {
		c.sessionTimer.Stop()
	}
	c.sessionTimer = time.AfterFunc(delay, c.runSessionRefresh)
	c.mu.Unlock()
}

func (c *Call) runSessionRefresh() {
	if c.GetState() != callstate.StateConnected {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), voiceSessionRefreshTimeout)
	err := c.agent.refreshVoiceSession(ctx, c)
	cancel()
	if err != nil {
		logging.WarnRate("ims-voice-session-refresh-failed", "IMS 会话刷新失败", "err", err)
	}
}
