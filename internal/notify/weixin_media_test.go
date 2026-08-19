package notify

import (
	"context"
	"crypto/aes"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
	"github.com/yibaiba/hideck/internal/config"
)

func TestWeixinChannelUploadsEncryptedRecordingAsFile(t *testing.T) {
	plaintext := []byte("ID3-real-mp3-content")
	path := filepath.Join(t.TempDir(), "call_modem-1.mp3")
	if err := os.WriteFile(path, plaintext, 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var uploadRequest map[string]any
	var ciphertext []byte
	var sentMessage map[string]any
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/getuploadurl":
			mu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&uploadRequest)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"ret": 0, "upload_full_url": provider.URL + "/upload"})
		case "/upload":
			mu.Lock()
			ciphertext, _ = io.ReadAll(r.Body)
			mu.Unlock()
			w.Header().Set("x-encrypted-param", "encrypted-query")
			w.WriteHeader(http.StatusOK)
		case "/ilink/bot/sendmessage":
			mu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&sentMessage)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"ret":0,"errcode":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json"))
	state := newRuntimeState()
	state.Weixin = WeixinRuntimeState{
		AccountID: "bot-1", Token: "token-1", BaseURL: provider.URL,
		ContextTokens: map[string]string{"user-1": "context-1"}, DefaultTarget: "user-1",
	}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	channel, err := NewWeixinChannel(WeixinChannelOptions{
		Config: config.WeixinConfig{Enabled: true, CDNBaseURL: provider.URL}, StateStore: store, HTTPClient: provider.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	attachment := CommandAttachment{
		Type: "audio", Recording: filepath.Base(path), ContentType: "audio/mpeg",
		Path: path, Codec: "MP3", Size: int64(len(plaintext)),
	}
	if err := channel.sendAttachment(context.Background(), "user-1", attachment); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	keyHex := uploadRequest["aeskey"].(string)
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		t.Fatal(err)
	}
	decrypted := decryptECBForTest(t, ciphertext, key)
	padding := int(decrypted[len(decrypted)-1])
	if string(decrypted[:len(decrypted)-padding]) != string(plaintext) {
		t.Fatalf("decrypted upload = %q", decrypted)
	}
	if uploadRequest["media_type"] != float64(weixinMediaFile) || uploadRequest["rawsize"] != float64(len(plaintext)) {
		t.Fatalf("upload request = %#v", uploadRequest)
	}
	message := sentMessage["msg"].(map[string]any)
	if message["context_token"] != "context-1" {
		t.Fatalf("message = %#v", message)
	}
	item := message["item_list"].([]any)[0].(map[string]any)
	fileItem := item["file_item"].(map[string]any)
	if item["type"] != float64(weixinItemFile) || fileItem["file_name"] != filepath.Base(path) || fileItem["len"] != strconv.Itoa(len(plaintext)) {
		t.Fatalf("file item = %#v", item)
	}
}

func TestWeixinChannelRejectsMissingRecordingFile(t *testing.T) {
	channel := &WeixinChannel{}
	err := channel.sendAttachment(context.Background(), "user-1", CommandAttachment{Path: filepath.Join(t.TempDir(), "missing.mp3")})
	if err == nil || !strings.Contains(err.Error(), "读取录音文件失败") {
		t.Fatalf("sendAttachment() error = %v", err)
	}
}

func TestWeixinCommandAttachmentFailureDoesNotSendCompletionText(t *testing.T) {
	var requests []string
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/getuploadurl":
			_ = json.NewEncoder(w).Encode(map[string]any{"ret": 0, "upload_full_url": provider.URL + "/upload"})
		case "/upload":
			w.Header().Set("x-encrypted-param", "encrypted-query")
		case "/ilink/bot/sendmessage":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			item := payload["msg"].(map[string]any)["item_list"].([]any)[0].(map[string]any)
			if item["type"] == float64(weixinItemFile) {
				requests = append(requests, "file")
				_, _ = w.Write([]byte(`{"ret":1,"errmsg":"file rejected"}`))
				return
			}
			text := item["text_item"].(map[string]any)["text"].(string)
			requests = append(requests, text)
			_, _ = w.Write([]byte(`{"ret":0,"errcode":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	channel := newWeixinMediaTestChannel(t, provider)
	path := filepath.Join(t.TempDir(), "call.mp3")
	if err := os.WriteFile(path, []byte("ID3-audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	commandContext := &weixinCommandContext{channel: channel, target: "user-1", released: true}
	commandContext.respondAndReport(weixinCommandReply{
		text: "呼叫完成", attachments: []CommandAttachment{{Path: path, Codec: "MP3"}},
	})
	if len(requests) != 2 || requests[0] != "file" || !strings.Contains(requests[1], "录音发送失败") ||
		strings.Contains(requests[1], "呼叫完成") {
		t.Fatalf("send requests = %#v", requests)
	}
}

func newWeixinMediaTestChannel(t *testing.T, provider *httptest.Server) *WeixinChannel {
	t.Helper()
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json"))
	state := newRuntimeState()
	state.Weixin = WeixinRuntimeState{
		AccountID: "bot-1", Token: "token-1", BaseURL: provider.URL,
		ContextTokens: map[string]string{"user-1": "context-1"}, DefaultTarget: "user-1",
	}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	channel, err := NewWeixinChannel(WeixinChannelOptions{
		Config: config.WeixinConfig{Enabled: true, CDNBaseURL: provider.URL}, StateStore: store, HTTPClient: provider.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return channel
}

func TestVoiceRecordingAttachmentCarriesPrivatePathAndMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "call_test.mp3")
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "call_test.amr")
	if err := os.WriteFile(sourcePath, []byte("#!AMR\nvoice"), 0o600); err != nil {
		t.Fatal(err)
	}
	attachment, ok := voiceRecordingAttachment(&voicehost.SimulateCallResult{
		AudioPath: path, AudioCodec: "MP3", SourceAudioPath: sourcePath, SourceAudioCodec: "AMR",
	})
	if !ok || attachment.Path != path || attachment.Size != 5 || attachment.Codec != "MP3" ||
		attachment.SourcePath != sourcePath || attachment.SourceCodec != "AMR" {
		t.Fatalf("attachment = %+v, ok = %v", attachment, ok)
	}
	encoded, err := json.Marshal(attachment)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), path) || strings.Contains(string(encoded), sourcePath) ||
		!strings.Contains(string(encoded), `"recording":"call_test.mp3"`) {
		t.Fatalf("attachment JSON = %s", encoded)
	}
}

func TestVoiceCallRecordingChannelsDoNotReuseOneShotRespond(t *testing.T) {
	if _, ok := any(&tgCommandContext{}).(commandAttachmentContext); !ok {
		t.Fatal("telegram must send vocall recordings as new messages")
	}
	if _, ok := any(&weixinCommandContext{}).(commandAttachmentContext); !ok {
		t.Fatal("personal weixin must send vocall recordings as new messages")
	}
	if _, ok := any(&weComCommandContext{}).(commandAttachmentContext); !ok {
		t.Fatal("wecom must send vocall recordings")
	}
}

func TestVoiceRecordingAttachmentFallsBackToAMRWhenMP3Missing(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "call_test.amr")
	if err := os.WriteFile(sourcePath, []byte("#!AMR\nvoice"), 0o600); err != nil {
		t.Fatal(err)
	}
	attachment, ok := voiceRecordingAttachment(&voicehost.SimulateCallResult{
		AudioPath: sourcePath, AudioCodec: "AMR",
	})
	if !ok || attachment.Path != sourcePath || attachment.SourcePath != sourcePath ||
		attachment.Codec != "AMR" || attachment.SourceCodec != "AMR" {
		t.Fatalf("attachment = %+v, ok = %v", attachment, ok)
	}
}

func decryptECBForTest(t *testing.T, ciphertext, key []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		t.Fatalf("ciphertext length = %d", len(ciphertext))
	}
	plaintext := make([]byte, len(ciphertext))
	for offset := 0; offset < len(ciphertext); offset += aes.BlockSize {
		block.Decrypt(plaintext[offset:offset+aes.BlockSize], ciphertext[offset:offset+aes.BlockSize])
	}
	return plaintext
}
