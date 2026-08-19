//go:build !(linux && arm)

package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/yibaiba/hideck/pkg/logger"
)

const feishuFileMaxSize = 30 * 1024 * 1024

type feishuMediaAPI interface {
	Upload(ctx context.Context, filename string, body io.Reader) (string, error)
	SendFile(ctx context.Context, chatID, fileKey string) error
}

type feishuSDKMedia struct {
	client *lark.Client
}

func (f *FeishuChannel) deliverCommandResult(msg *larkim.EventMessage, text string, attachments []CommandAttachment) {
	f.replyToMessage(msg, text)
	if len(attachments) == 0 {
		return
	}
	var notes []string
	for _, attachment := range attachments {
		if err := f.sendRecording(msg, attachment); err != nil {
			logger.Warn("发送飞书录音失败", "err", err)
			notes = append(notes, err.Error())
		}
	}
	if len(notes) > 0 {
		f.replyToMessage(msg, "录音发送失败\n原因    "+strings.Join(notes, "；"))
	}
}

func (f *FeishuChannel) sendRecording(msg *larkim.EventMessage, attachment CommandAttachment) error {
	if f == nil {
		return errors.New("飞书渠道未初始化")
	}
	path, name, err := feishuRecordingFile(attachment)
	if err != nil {
		return err
	}
	chatID := feishuMessageChatID(msg)
	if chatID == "" && len(f.chatIDs) > 0 {
		chatID = f.chatIDs[0]
	}
	if chatID == "" {
		return errors.New("飞书会话缺少 Chat ID")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开飞书录音失败: %w", err)
	}
	defer file.Close()
	media := f.media
	if media == nil {
		media = feishuSDKMedia{client: f.client}
	}
	fileKey, err := media.Upload(context.Background(), name, file)
	if err != nil {
		return err
	}
	return media.SendFile(context.Background(), chatID, fileKey)
}

func feishuRecordingFile(attachment CommandAttachment) (string, string, error) {
	candidates := []struct {
		path string
		name string
	}{
		{strings.TrimSpace(attachment.Path), strings.TrimSpace(attachment.Recording)},
		{strings.TrimSpace(attachment.SourcePath), ""},
	}
	for _, candidate := range candidates {
		if candidate.path == "" {
			continue
		}
		info, err := os.Stat(candidate.path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
			continue
		}
		if info.Size() > feishuFileMaxSize {
			return "", "", fmt.Errorf("录音文件大小 %d 超过飞书 30 MiB 限制", info.Size())
		}
		name := candidate.name
		if name == "" {
			name = filepath.Base(candidate.path)
		}
		return candidate.path, name, nil
	}
	return "", "", errors.New("录音文件不可用")
}

func feishuMessageChatID(msg *larkim.EventMessage) string {
	if msg == nil || msg.ChatId == nil {
		return ""
	}
	return strings.TrimSpace(*msg.ChatId)
}

func (m feishuSDKMedia) Upload(ctx context.Context, filename string, body io.Reader) (string, error) {
	if m.client == nil {
		return "", errors.New("飞书客户端未初始化")
	}
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = "recording.bin"
	}
	req := larkim.NewCreateFileReqBuilder().
		Body(larkim.NewCreateFileReqBodyBuilder().
			FileType(larkim.FileTypeStream).
			FileName(filename).
			File(body).
			Build()).
		Build()
	resp, err := m.client.Im.File.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("上传飞书录音失败: %w", err)
	}
	if !resp.Success() || resp.Data == nil || strings.TrimSpace(ptrText(resp.Data.FileKey)) == "" {
		return "", fmt.Errorf("上传飞书录音失败: %s", feishuAPIError(resp.Code, resp.Msg))
	}
	return strings.TrimSpace(*resp.Data.FileKey), nil
}

func (m feishuSDKMedia) SendFile(ctx context.Context, chatID, fileKey string) error {
	if m.client == nil {
		return errors.New("飞书客户端未初始化")
	}
	content, err := json.Marshal(map[string]string{"file_key": fileKey})
	if err != nil {
		return err
	}
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("file").
			Content(string(content)).
			Build()).
		Build()
	resp, err := m.client.Im.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("发送飞书录音失败: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("发送飞书录音失败: %s", feishuAPIError(resp.Code, resp.Msg))
	}
	return nil
}

func feishuAPIError(code int, message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return fmt.Sprintf("飞书 API 错误 %d", code)
	}
	return fmt.Sprintf("飞书 API 错误 %d: %s", code, message)
}

func ptrText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
