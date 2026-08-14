package notify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yibaiba/hideck/pkg/logger"
)

func (w *WeComBotChannel) serveConnection(ctx context.Context) (bool, error) {
	conn, err := w.openConnection(ctx)
	if err != nil {
		return false, err
	}
	w.setConnection(conn)
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		w.heartbeatLoop(heartbeatCtx, conn)
	}()
	readErr := w.readLoop(ctx, conn)
	cancelHeartbeat()
	_ = conn.Close()
	<-heartbeatDone
	w.clearConnection(conn)
	w.failPending(errors.New("企业微信长连接已中断"))
	return true, readErr
}

func (w *WeComBotChannel) openConnection(ctx context.Context) (WeComBotConnection, error) {
	connectCtx, cancel := context.WithTimeout(ctx, w.connectTimeout)
	defer cancel()
	conn, response, err := w.dialer.DialContext(connectCtx, w.config.WebSocketURL, http.Header{})
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, fmt.Errorf("连接企业微信 WebSocket 失败: %w", redactWeComTransportError(err))
	}
	if err := w.authenticate(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (w *WeComBotChannel) authenticate(conn WeComBotConnection) error {
	requestID, err := w.newRequestID("subscribe")
	if err != nil {
		return err
	}
	deadline := w.now().Add(w.connectTimeout)
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	if err := conn.WriteJSON(map[string]any{
		"cmd": weComCommandSubscribe, "headers": map[string]string{"req_id": requestID},
		"body": map[string]string{"bot_id": w.config.BotID, "secret": w.config.Secret, "device_id": w.deviceID},
	}); err != nil {
		return fmt.Errorf("发送企业微信订阅请求失败: %w", err)
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return err
	}
	defer conn.SetReadDeadline(time.Time{})
	for {
		var frame weComFrame
		if err := conn.ReadJSON(&frame); err != nil {
			return fmt.Errorf("等待企业微信订阅响应失败: %w", err)
		}
		if frame.Command == weComCommandPing {
			continue
		}
		if frame.Headers.RequestID != requestID {
			continue
		}
		return validateWeComFrame(frame, "企业微信订阅")
	}
}

func (w *WeComBotChannel) readLoop(ctx context.Context, conn WeComBotConnection) error {
	for ctx.Err() == nil {
		var frame weComFrame
		if err := conn.ReadJSON(&frame); err != nil {
			if ctx.Err() != nil || errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("读取企业微信 WebSocket 失败: %w", err)
		}
		w.dispatchFrame(ctx, frame)
	}
	return nil
}

func (w *WeComBotChannel) dispatchFrame(ctx context.Context, frame weComFrame) {
	if frame.Headers.RequestID != "" && !isWeComNonResponse(frame.Command) && w.deliverPending(frame) {
		return
	}
	if isWeComCallback(frame.Command) {
		go func() {
			if err := w.processCallback(ctx, frame); err != nil && ctx.Err() == nil {
				logger.Warn("处理企业微信长连接消息失败", "err", err)
			}
		}()
	}
}

func (w *WeComBotChannel) heartbeatLoop(ctx context.Context, conn WeComBotConnection) {
	ticker := time.NewTicker(w.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			requestID, err := w.newRequestID("ping")
			if err != nil {
				logger.Warn("生成企业微信心跳 ID 失败", "err", err)
				continue
			}
			if err := w.writeFrame(conn, map[string]any{
				"cmd": weComCommandPing, "headers": map[string]string{"req_id": requestID}, "body": map[string]any{},
			}); err != nil && ctx.Err() == nil {
				logger.Warn("发送企业微信心跳失败", "err", err)
			}
		}
	}
}

func (w *WeComBotChannel) sendRequest(
	ctx context.Context,
	command string,
	requestID string,
	body map[string]any,
) (weComFrame, error) {
	if strings.TrimSpace(requestID) == "" {
		var err error
		requestID, err = w.newRequestID(command)
		if err != nil {
			return weComFrame{}, err
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, w.requestTimeout)
	defer cancel()
	resultCh := make(chan weComPendingResult, 1)
	if err := w.addPending(requestID, resultCh); err != nil {
		return weComFrame{}, err
	}
	defer w.removePending(requestID)
	conn := w.currentConnection()
	if conn == nil {
		return weComFrame{}, errors.New("企业微信 WebSocket 尚未连接")
	}
	if err := w.writeFrame(conn, map[string]any{
		"cmd": command, "headers": map[string]string{"req_id": requestID}, "body": body,
	}); err != nil {
		return weComFrame{}, err
	}
	select {
	case <-waitCtx.Done():
		return weComFrame{}, fmt.Errorf("等待企业微信 %s 响应失败: %w", command, waitCtx.Err())
	case result := <-resultCh:
		if result.err != nil {
			return weComFrame{}, result.err
		}
		if err := validateWeComFrame(result.frame, command); err != nil {
			return weComFrame{}, err
		}
		return result.frame, nil
	}
}

func (w *WeComBotChannel) writeFrame(conn WeComBotConnection, frame any) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	if conn == nil {
		return errors.New("企业微信 WebSocket 尚未连接")
	}
	if err := conn.SetWriteDeadline(w.now().Add(w.requestTimeout)); err != nil {
		return err
	}
	if err := conn.WriteJSON(frame); err != nil {
		return fmt.Errorf("写入企业微信 WebSocket 失败: %w", err)
	}
	return nil
}

func (w *WeComBotChannel) addPending(requestID string, result chan weComPendingResult) error {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()
	if _, exists := w.pending[requestID]; exists {
		return fmt.Errorf("企业微信请求 ID 正在使用: %s", requestID)
	}
	w.pending[requestID] = result
	return nil
}

func (w *WeComBotChannel) removePending(requestID string) {
	w.pendingMu.Lock()
	delete(w.pending, requestID)
	w.pendingMu.Unlock()
}

func (w *WeComBotChannel) deliverPending(frame weComFrame) bool {
	w.pendingMu.Lock()
	result := w.pending[frame.Headers.RequestID]
	w.pendingMu.Unlock()
	if result == nil {
		return false
	}
	select {
	case result <- weComPendingResult{frame: frame}:
	default:
	}
	return true
}

func (w *WeComBotChannel) failPending(err error) {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()
	for requestID, result := range w.pending {
		select {
		case result <- weComPendingResult{err: err}:
		default:
		}
		delete(w.pending, requestID)
	}
}

func (w *WeComBotChannel) setConnection(conn WeComBotConnection) {
	w.connMu.Lock()
	w.conn = conn
	w.connMu.Unlock()
}

func (w *WeComBotChannel) clearConnection(conn WeComBotConnection) {
	w.connMu.Lock()
	if w.conn == conn {
		w.conn = nil
	}
	w.connMu.Unlock()
}

func (w *WeComBotChannel) newRequestID(prefix string) (string, error) {
	value, err := randomHex(16)
	if err != nil {
		return "", fmt.Errorf("生成企业微信请求 ID 失败: %w", err)
	}
	return strings.TrimSpace(prefix) + "-" + value, nil
}

func validateWeComFrame(frame weComFrame, operation string) error {
	if frame.ErrCode == 0 {
		return nil
	}
	return fmt.Errorf("%s 失败: errcode=%d errmsg=%s", operation, frame.ErrCode, frame.ErrMsg)
}
