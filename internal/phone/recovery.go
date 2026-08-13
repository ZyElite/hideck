package phone

import (
	"context"
	"time"

	"github.com/iniwex5/vohive/pkg/logger"
	"github.com/pion/webrtc/v4"
)

const disconnectHangupTimeout = 10 * time.Second

func (s *Service) handleMediaState(mediaID string, state webrtc.PeerConnectionState) {
	if state == webrtc.PeerConnectionStateConnected {
		s.cancelDisconnectTimer(mediaID)
		return
	}
	if state == webrtc.PeerConnectionStateDisconnected ||
		state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
		s.scheduleDisconnectHangup(mediaID)
	}
}

func (s *Service) cancelDisconnectTimer(mediaID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	call := s.calls[s.mediaCalls[mediaID]]
	if call != nil && call.disconnectTimer != nil {
		call.disconnectTimer.Stop()
		call.disconnectTimer = nil
	}
}

func (s *Service) scheduleDisconnectHangup(mediaID string) {
	s.mu.Lock()
	callID := s.mediaCalls[mediaID]
	call := s.calls[callID]
	if call == nil || call.terminal || call.mediaID != mediaID || call.disconnectTimer != nil {
		s.mu.Unlock()
		return
	}
	call.disconnectTimer = time.AfterFunc(s.recoveryGrace, func() { s.expireDisconnectedMedia(callID, mediaID) })
	s.mu.Unlock()
	s.publish("media_disconnected", call)
}

func (s *Service) expireDisconnectedMedia(callID, mediaID string) {
	s.mu.RLock()
	call := s.calls[callID]
	valid := call != nil && !call.terminal && call.mediaID == mediaID
	deviceID, resolvedCallID := "", ""
	if valid {
		deviceID, resolvedCallID = call.view.DeviceID, call.view.CallID
	}
	s.mu.RUnlock()
	if !valid {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, disconnectHangupTimeout)
	defer cancel()
	if err := s.gateway.HangupCall(ctx, deviceID, resolvedCallID); err != nil {
		logger.Error("媒体恢复超时挂断失败", "device_id", deviceID, "call_id", resolvedCallID, "err", err)
	}
}
