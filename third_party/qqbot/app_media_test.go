package qqbot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/iniwex5/qqbot/internal/rest"
)

type mediaTestTokens struct{}

func (mediaTestTokens) Token(context.Context) (string, error) { return "token", nil }
func (mediaTestTokens) Invalidate()                           {}

func TestAppSendVoiceMapsDeliveryToMediaProtocol(t *testing.T) {
	path := filepath.Join(t.TempDir(), "call.mp3")
	if err := os.WriteFile(path, []byte("mp3-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	var upload map[string]any
	var message map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/groups/group-1/files":
			_ = json.NewDecoder(r.Body).Decode(&upload)
			_, _ = w.Write([]byte(`{"file_info":"file-token"}`))
		case "/v2/groups/group-1/messages":
			_ = json.NewDecoder(r.Body).Decode(&message)
			_, _ = w.Write([]byte(`{"id":"reply-1","timestamp":"2026-08-14T12:00:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := &App{delivery: rest.New(rest.Config{
		BaseURL: server.URL, Client: server.Client(), Tokens: mediaTestTokens{},
	})}

	receipt, err := app.Send(context.Background(), Delivery{
		To: Recipient{Kind: GroupRecipient, ID: "group-1"}, Kind: Voice,
		MediaPath: path, FileName: "recording.mp3",
		Reply: &ReplyContext{MessageID: "msg-1", Sequence: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	media, _ := message["media"].(map[string]any)
	if receipt.ID != "reply-1" || int(message["msg_type"].(float64)) != 7 ||
		media["file_info"] != "file-token" || message["msg_id"] != "msg-1" ||
		int(message["msg_seq"].(float64)) != 3 ||
		upload["file_data"] != base64.StdEncoding.EncodeToString([]byte("mp3-data")) {
		t.Fatalf("receipt = %+v, upload = %#v, message = %#v", receipt, upload, message)
	}
}

func TestAppSendVoiceRejectsChannelRecipient(t *testing.T) {
	app := &App{}
	_, err := app.Send(context.Background(), Delivery{
		To: Recipient{Kind: ChannelRecipient, ID: "channel-1"}, Kind: Voice, MediaPath: "call.mp3",
	})
	if err == nil {
		t.Fatal("expected unsupported channel media error")
	}
}
