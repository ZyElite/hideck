package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	weComCommandSubscribe    = "aibot_subscribe"
	weComCommandCallback     = "aibot_msg_callback"
	weComCommandLegacy       = "aibot_callback"
	weComCommandEvent        = "aibot_event_callback"
	weComCommandSend         = "aibot_send_msg"
	weComCommandRespond      = "aibot_respond_msg"
	weComCommandPing         = "ping"
	weComCommandUploadInit   = "aibot_upload_media_init"
	weComCommandUploadChunk  = "aibot_upload_media_chunk"
	weComCommandUploadFinish = "aibot_upload_media_finish"
	weComMaxMessageRunes     = 4000
)

var defaultWeComReconnectBackoff = []time.Duration{
	2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, 60 * time.Second,
}

type weComFrame struct {
	Command string          `json:"cmd"`
	Headers weComHeaders    `json:"headers"`
	Body    json.RawMessage `json:"body"`
	ErrCode int             `json:"errcode"`
	ErrMsg  string          `json:"errmsg"`
}

type weComHeaders struct {
	RequestID string `json:"req_id"`
}

type weComPendingResult struct {
	frame weComFrame
	err   error
}

type WeComBotConnection interface {
	ReadJSON(target any) error
	WriteJSON(value any) error
	SetReadDeadline(deadline time.Time) error
	SetWriteDeadline(deadline time.Time) error
	Close() error
}

type WeComBotDialer interface {
	DialContext(ctx context.Context, endpoint string, headers http.Header) (WeComBotConnection, *http.Response, error)
}

type gorillaWeComBotDialer struct {
	dialer *websocket.Dialer
}

func newGorillaWeComBotDialer(connectTimeout time.Duration) WeComBotDialer {
	return &gorillaWeComBotDialer{dialer: &websocket.Dialer{
		Proxy: http.ProxyFromEnvironment, HandshakeTimeout: connectTimeout,
	}}
}

func (d *gorillaWeComBotDialer) DialContext(
	ctx context.Context,
	endpoint string,
	headers http.Header,
) (WeComBotConnection, *http.Response, error) {
	return d.dialer.DialContext(ctx, endpoint, headers)
}

func isWeComCallback(command string) bool {
	return command == weComCommandCallback || command == weComCommandLegacy
}

func isWeComNonResponse(command string) bool {
	return isWeComCallback(command) || command == weComCommandEvent
}
