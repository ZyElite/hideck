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
	c.SessionTimerMu.Lock()
	c.SessionExpires = int(expires / time.Second)
	c.SessionTimerMu.Unlock()
}

func (c *Call) voiceSessionExpires() time.Duration {
	if c == nil {
		return 0
	}
	c.SessionTimerMu.Lock()
	defer c.SessionTimerMu.Unlock()
	return time.Duration(c.SessionExpires) * time.Second
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

// StartSessionTimer retains the v1.5.5 callback-based timer API.
func (c *Call) StartSessionTimer(callback func()) {
	if c == nil {
		return
	}
	c.SessionTimerMu.Lock()
	defer c.SessionTimerMu.Unlock()
	if c.SessionExpires < 1 {
		return
	}
	if c.SessionTimer != nil {
		c.SessionTimer.Stop()
	}
	delay := sessionRefreshDelay(time.Duration(c.SessionExpires) * time.Second)
	c.SessionTimer = time.AfterFunc(delay, func() {
		if callback != nil {
			callback()
		}
	})
}

// StartSessionTimerCurrent retains the additive duration-based API.
func (c *Call) StartSessionTimerCurrent(expires time.Duration) error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	if expires > 0 {
		c.SessionTimerMu.Lock()
		c.SessionExpires = int(expires / time.Second)
		c.SessionTimerMu.Unlock()
	}
	if c.voiceSessionExpires() <= 0 {
		return c.stopSessionTimer()
	}
	if c.agent == nil || c.agent.ims == nil {
		return errors.New("voice: session timer has no IMS agent")
	}
	c.agent.startVoiceSessionTimer(c)
	return nil
}

func (a *Agent) startVoiceSessionTimer(call *Call) {
	if a == nil || call == nil {
		return
	}
	call.StartSessionTimer(func() {
		a.runCallTask(call, "session_update", func() { a.sendIMSSessionUpdate(call) })
	})
}

func (a *Agent) sendIMSSessionUpdate(call *Call) {
	if a == nil || call == nil || call.CallState() != callstate.StateConnected {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), voiceSessionRefreshTimeout)
	defer cancel()
	err := a.refreshVoiceSession(ctx, call)
	if err != nil {
		logging.WarnRate("ims-voice-session-refresh-failed:"+call.CallID(),
			"IMS 会话刷新失败", "device", a.deviceID, "call_id", call.CallID(), "err", err)
	}
}
