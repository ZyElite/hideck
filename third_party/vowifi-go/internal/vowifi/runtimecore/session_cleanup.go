package runtimecore

import (
	"context"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"go.uber.org/zap"
)

const sessionCleanupTimeout = 5 * time.Second

func defaultSessionStarter(ctx context.Context, cfg SessionConfig) (*SessionResult, error) {
	return RunSession(ctx, cfg)
}

// StopSession releases every resource owned by a completed session start.
func StopSession(ctx context.Context, result *SessionResult) {
	defaultStopSession(ctx, result)
}

func defaultStopSession(ctx context.Context, result *SessionResult) {
	if result == nil {
		return
	}
	if result.IMSService != nil {
		_ = result.IMSService.Stop(ctx)
	}
	if result.EPDGMgr != nil {
		if err := result.EPDGMgr.Stop(result.DeviceID); err != nil && !strings.Contains(err.Error(), "session id") {
			zap.S().Warnw("failed to stop ePDG session", "device", result.DeviceID, "error", err)
		}
	}
	if result.IMSNetwork != nil {
		_ = result.IMSNetwork.Close()
	}
	waitSessionCleanup(result.Session, result.DeviceID)
	CleanupDataplaneInterface(result.DeviceID, result.Snapshot.TUNName)
}

func waitSessionCleanup(session *swu.Session, deviceID string) {
	if session == nil {
		return
	}
	timer := time.NewTimer(sessionCleanupTimeout)
	defer timer.Stop()
	select {
	case <-sessionDoneChan(session):
	case <-timer.C:
		zap.S().Warnw("SWu session cleanup timed out", "device", strings.TrimSpace(deviceID))
	}
}

func sessionDoneChan(session *swu.Session) <-chan struct{} {
	if session == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		session.WaitDone()
		close(done)
	}()
	return done
}
