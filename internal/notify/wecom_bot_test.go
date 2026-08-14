package notify

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yibaiba/hideck/internal/config"
)

func TestWeComBotChannelBindsDirectCommandAndSendsNotification(t *testing.T) {
	commandReply := make(chan map[string]any, 1)
	notification := make(chan map[string]any, 1)
	provider := newWeComWebSocketServer(t, func(conn *websocket.Conn, _ int) {
		authenticateWeComTestConnection(t, conn)
		writeWeComTestFrame(t, conn, map[string]any{
			"cmd": weComCommandCallback, "headers": map[string]string{"req_id": "callback-1"},
			"body": map[string]any{
				"msgid": "message-1", "chatid": "direct-1", "chattype": "single", "msgtype": "text",
				"from": map[string]string{"userid": "user-1"}, "text": map[string]string{"content": "/help"},
			},
		})
		for {
			frame, err := readWeComTestFrame(conn)
			if err != nil {
				return
			}
			switch frameCommand(frame) {
			case weComCommandRespond:
				commandReply <- frame
				ackWeComTestFrame(t, conn, frame)
			case weComCommandSend:
				notification <- frame
				ackWeComTestFrame(t, conn, frame)
			}
		}
	})
	defer provider.Close()
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json"))
	channel := newWeComBotTestChannel(t, provider, store, nil)
	channel.RegisterCommand("help", func(CommandContext, []string) string { return "设备 ID    modem-1" })
	startDone := startWeComBotTestChannel(channel)

	reply := receiveWeComTestFrame(t, commandReply)
	if frameRequestID(reply) != "callback-1" || !strings.Contains(frameMarkdown(reply), "modem-1") {
		t.Fatalf("command reply = %#v", reply)
	}
	waitUntil(t, time.Second, func() bool {
		state, err := store.Load()
		return err == nil && state.WeComBot.DefaultTarget == "direct-1" && containsString(state.WeComBot.AllowedUsers, "user-1")
	})
	if err := channel.Send("来电通知"); err != nil {
		t.Fatal(err)
	}
	outbound := receiveWeComTestFrame(t, notification)
	if frameChatID(outbound) != "direct-1" || frameMarkdown(outbound) != "来电通知" {
		t.Fatalf("notification = %#v", outbound)
	}
	closeWeComBotTestChannel(t, channel, startDone)
}

func TestWeComBotChannelCorrelatesConcurrentResponses(t *testing.T) {
	provider := newWeComWebSocketServer(t, func(conn *websocket.Conn, _ int) {
		authenticateWeComTestConnection(t, conn)
		first, err := readWeComTestFrame(conn)
		if err != nil {
			return
		}
		second, err := readWeComTestFrame(conn)
		if err != nil {
			return
		}
		ackWeComTestFrame(t, conn, second)
		ackWeComTestFrame(t, conn, first)
	})
	defer provider.Close()
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json"))
	state := newRuntimeState()
	state.WeComBot.DefaultTarget = "direct-1"
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	channel := newWeComBotTestChannel(t, provider, store, nil)
	startDone := startWeComBotTestChannel(channel)
	waitUntil(t, time.Second, func() bool { return channel.currentConnection() != nil })

	errorsCh := make(chan error, 2)
	go func() { errorsCh <- channel.Send("first") }()
	go func() { errorsCh <- channel.Send("second") }()
	for range 2 {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
	}
	closeWeComBotTestChannel(t, channel, startDone)
}

func TestWeComBotChannelReconnectsAndSendsHeartbeat(t *testing.T) {
	var connections atomic.Int32
	heartbeat := make(chan struct{}, 1)
	provider := newWeComWebSocketServer(t, func(conn *websocket.Conn, connection int) {
		connections.Add(1)
		authenticateWeComTestConnection(t, conn)
		if connection == 1 {
			_ = conn.Close()
			return
		}
		for {
			frame, err := readWeComTestFrame(conn)
			if err != nil {
				return
			}
			if frameCommand(frame) == weComCommandPing {
				select {
				case heartbeat <- struct{}{}:
				default:
				}
			}
		}
	})
	defer provider.Close()
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json"))
	channel := newWeComBotTestChannel(t, provider, store, func(options *WeComBotOptions) {
		options.HeartbeatInterval = 10 * time.Millisecond
		options.ReconnectBackoff = []time.Duration{5 * time.Millisecond}
	})
	startDone := startWeComBotTestChannel(channel)
	select {
	case <-heartbeat:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat not observed after reconnect")
	}
	if connections.Load() < 2 {
		t.Fatalf("connections = %d", connections.Load())
	}
	closeWeComBotTestChannel(t, channel, startDone)
}

func TestWeComBotGroupCannotClaimFirstBinding(t *testing.T) {
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json"))
	channel, err := NewWeComBotChannel(WeComBotOptions{
		Config: config.WeComBotConfig{
			Enabled: true, BotID: "bot-1", Secret: "secret-1", WebSocketURL: "ws://127.0.0.1/socket",
			AllowedGroupIDs: []string{"group-1"},
		},
		StateStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"msgid": "message-1", "chatid": "group-1", "chattype": "group", "msgtype": "text",
		"from": map[string]string{"userid": "user-1"}, "text": map[string]string{"content": "/help"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := channel.processCallback(context.Background(), weComFrame{
		Command: weComCommandCallback, Headers: weComHeaders{RequestID: "callback-1"}, Body: body,
	}); err != nil {
		t.Fatal(err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.WeComBot.DefaultTarget != "" || len(state.WeComBot.AllowedUsers) != 0 {
		t.Fatalf("group claimed binding: %+v", state.WeComBot)
	}
}

func TestWeComBotDirectAllowlistRejectsUnknownAndSetsKnownDefault(t *testing.T) {
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json"))
	channel, err := NewWeComBotChannel(WeComBotOptions{
		Config: config.WeComBotConfig{
			Enabled: true, BotID: "bot-1", Secret: "secret-1", WebSocketURL: "ws://127.0.0.1/socket",
			AllowedUserIDs: []string{"known-user"},
		},
		StateStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	unknown, bound, err := channel.authorizeMessage(weComAuthorizationRequest{
		kind: "direct", chatID: "unknown-chat", sender: "unknown-user",
	})
	if err != nil || unknown || bound {
		t.Fatalf("unknown allowed = %v, bound = %v, error = %v", unknown, bound, err)
	}
	known, bound, err := channel.authorizeMessage(weComAuthorizationRequest{
		kind: "direct", chatID: "known-chat", sender: "known-user",
	})
	if err != nil || !known || !bound {
		t.Fatalf("known allowed = %v, bound = %v, error = %v", known, bound, err)
	}
	state, err := store.Load()
	if err != nil || state.WeComBot.DefaultTarget != "known-chat" {
		t.Fatalf("state = %+v, error = %v", state.WeComBot, err)
	}
}

func TestWeComBotChannelRejectsMissingCredentials(t *testing.T) {
	_, err := NewWeComBotChannel(WeComBotOptions{
		Config:     config.WeComBotConfig{Enabled: true, BotID: "bot-1"},
		StateStore: NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json")),
	})
	if err == nil || !strings.Contains(err.Error(), "Secret") {
		t.Fatalf("error = %v", err)
	}
}

func TestWeComBotAuthenticationErrorDoesNotExposeSecret(t *testing.T) {
	provider := newWeComWebSocketServer(t, func(conn *websocket.Conn, _ int) {
		frame, err := readWeComTestFrame(conn)
		if err != nil {
			return
		}
		writeWeComTestFrame(t, conn, map[string]any{
			"cmd": weComCommandSubscribe, "headers": map[string]string{"req_id": frameRequestID(frame)},
			"body": map[string]any{}, "errcode": 40013, "errmsg": "invalid credential",
		})
	})
	defer provider.Close()
	channel := newWeComBotTestChannel(
		t, provider, NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json")), nil,
	)
	_, err := channel.openConnection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "40013") || strings.Contains(err.Error(), "secret-1") {
		t.Fatalf("error = %v", err)
	}
}
