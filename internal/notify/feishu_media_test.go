//go:build !(linux && arm)

package notify

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type fakeFeishuMedia struct {
	filename  string
	payload   []byte
	chatID    string
	fileKey   string
	uploadErr error
	sendErr   error
}

func (f *fakeFeishuMedia) Upload(_ context.Context, filename string, body io.Reader) (string, error) {
	f.filename = filename
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	f.payload = data
	if f.uploadErr != nil {
		return "", f.uploadErr
	}
	return "file-key-1", nil
}

func (f *fakeFeishuMedia) SendFile(_ context.Context, chatID, fileKey string) error {
	f.chatID, f.fileKey = chatID, fileKey
	return f.sendErr
}

func TestFeishuCommandContextSendsRecordingAfterText(t *testing.T) {
	if _, ok := any(&feishuCommandContext{}).(commandAttachmentContext); !ok {
		t.Fatal("feishu must implement recording upload")
	}
	path := filepath.Join(t.TempDir(), "call.mp3")
	if err := os.WriteFile(path, []byte("ID3-audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	media := &fakeFeishuMedia{}
	var texts []string
	chatID := "oc_group"
	channel := &FeishuChannel{
		media: media,
		replyText: func(_ *larkim.EventMessage, text string) {
			texts = append(texts, text)
		},
	}
	ctx := &feishuCommandContext{channel: channel, msg: &larkim.EventMessage{ChatId: &chatID}}
	replyVoiceCallCompletion(ctx, "呼叫完成", &voicehost.SimulateCallResult{
		Success: true, AudioPath: path, AudioCodec: "MP3",
	})
	if len(texts) != 1 || texts[0] != "呼叫完成\n录音    call.mp3" {
		t.Fatalf("texts = %#v", texts)
	}
	if media.filename != "call.mp3" || string(media.payload) != "ID3-audio" || media.chatID != chatID || media.fileKey != "file-key-1" {
		t.Fatalf("media = %+v", media)
	}
}

func TestFeishuRecordingFailureKeepsCompletionText(t *testing.T) {
	media := &fakeFeishuMedia{uploadErr: errors.New("upload rejected")}
	var texts []string
	chatID := "oc_group"
	channel := &FeishuChannel{
		media: media,
		replyText: func(_ *larkim.EventMessage, text string) {
			texts = append(texts, text)
		},
	}
	ctx := &feishuCommandContext{channel: channel, msg: &larkim.EventMessage{ChatId: &chatID}}
	ctx.ReplyWithAttachments("呼叫完成", []CommandAttachment{{Path: "/missing.mp3", Codec: "MP3", Recording: "missing.mp3"}})
	if len(texts) != 2 || texts[0] != "呼叫完成" || !strings.Contains(texts[1], "录音发送失败") || strings.Contains(texts[1], "呼叫完成") {
		t.Fatalf("texts = %#v", texts)
	}
}

func TestFeishuRecordingFallsBackToAMRSource(t *testing.T) {
	source := filepath.Join(t.TempDir(), "call.amr")
	if err := os.WriteFile(source, []byte("#!AMR\nvoice"), 0o600); err != nil {
		t.Fatal(err)
	}
	media := &fakeFeishuMedia{}
	channel := &FeishuChannel{
		media:     media,
		replyText: func(*larkim.EventMessage, string) {},
	}
	ctx := &feishuCommandContext{channel: channel, msg: &larkim.EventMessage{ChatId: strPtr("oc_group")}}
	ctx.ReplyWithAttachments("呼叫完成", []CommandAttachment{
		{Path: filepath.Join(t.TempDir(), "missing.mp3"), Codec: "MP3", SourcePath: source, SourceCodec: "AMR"},
	})
	if media.filename != "call.amr" || string(media.payload) != "#!AMR\nvoice" {
		t.Fatalf("media = %+v payload=%q", media, media.payload)
	}
}

func strPtr(value string) *string { return &value }
