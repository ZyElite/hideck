package notify

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yibaiba/hideck/internal/config"
)

func newWeComBotTestChannel(
	t *testing.T,
	provider *httptest.Server,
	store RuntimeStateStore,
	modify func(*WeComBotOptions),
) *WeComBotChannel {
	t.Helper()
	options := WeComBotOptions{
		Config: config.WeComBotConfig{
			Enabled: true, BotID: "bot-1", Secret: "secret-1",
			WebSocketURL: "ws" + strings.TrimPrefix(provider.URL, "http") + "/socket",
		},
		StateStore: store, HeartbeatInterval: time.Hour, RequestTimeout: time.Second,
		ConnectTimeout: time.Second, ReconnectBackoff: []time.Duration{time.Second},
	}
	if modify != nil {
		modify(&options)
	}
	channel, err := NewWeComBotChannel(options)
	if err != nil {
		t.Fatal(err)
	}
	return channel
}

func newWeComWebSocketServer(t *testing.T, handle func(*websocket.Conn, int)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var connectionMu sync.Mutex
	connectionNumber := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		connectionMu.Lock()
		connectionNumber++
		current := connectionNumber
		connectionMu.Unlock()
		handle(conn, current)
	}))
}

func authenticateWeComTestConnection(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	frame, err := readWeComTestFrame(conn)
	if err != nil {
		t.Errorf("read subscribe: %v", err)
		return
	}
	if frameCommand(frame) != weComCommandSubscribe || frameBody(frame)["secret"] != "secret-1" {
		t.Errorf("subscribe = %#v", frame)
		return
	}
	ackWeComTestFrame(t, conn, frame)
}

func readWeComTestFrame(conn *websocket.Conn) (map[string]any, error) {
	var frame map[string]any
	err := conn.ReadJSON(&frame)
	return frame, err
}

func writeWeComTestFrame(t *testing.T, conn *websocket.Conn, frame map[string]any) {
	t.Helper()
	if err := conn.WriteJSON(frame); err != nil {
		t.Errorf("write frame: %v", err)
	}
}

func ackWeComTestFrame(t *testing.T, conn *websocket.Conn, frame map[string]any) {
	t.Helper()
	writeWeComTestFrame(t, conn, map[string]any{
		"cmd": frameCommand(frame), "headers": map[string]string{"req_id": frameRequestID(frame)},
		"body": map[string]any{}, "errcode": 0,
	})
}

func frameCommand(frame map[string]any) string {
	value, _ := frame["cmd"].(string)
	return value
}

func frameRequestID(frame map[string]any) string {
	headers, _ := frame["headers"].(map[string]any)
	value, _ := headers["req_id"].(string)
	return value
}

func frameBody(frame map[string]any) map[string]any {
	body, _ := frame["body"].(map[string]any)
	return body
}

func frameMarkdown(frame map[string]any) string {
	markdown, _ := frameBody(frame)["markdown"].(map[string]any)
	value, _ := markdown["content"].(string)
	return value
}

func frameChatID(frame map[string]any) string {
	value, _ := frameBody(frame)["chatid"].(string)
	return value
}

func startWeComBotTestChannel(channel *WeComBotChannel) <-chan error {
	done := make(chan error, 1)
	go func() { done <- channel.Start() }()
	return done
}

func closeWeComBotTestChannel(t *testing.T, channel *WeComBotChannel, started <-chan error) {
	t.Helper()
	if err := channel.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("WeCom channel did not stop")
	}
}

func receiveWeComTestFrame(t *testing.T, frames <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for WeCom frame")
		return nil
	}
}
