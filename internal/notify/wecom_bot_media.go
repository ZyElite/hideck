package notify

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	weComUploadChunkSize = 512 * 1024
	weComVoiceMaxSize    = 2 * 1024 * 1024
	weComFileMaxSize     = 20 * 1024 * 1024
	weComMaxUploadChunks = 100
)

type weComMediaPlan struct {
	path      string
	filename  string
	mediaType string
	size      int64
	note      string
}

type weComUploadedMedia struct {
	mediaType string
	mediaID   string
}

func prepareWeComMedia(attachment CommandAttachment) (weComMediaPlan, error) {
	sourceCodec := strings.ToUpper(strings.TrimSpace(attachment.SourceCodec))
	fallbackNote := ""
	switch sourceCodec {
	case "AMR":
		source, sourceErr := weComFilePlan(attachment.SourcePath, "", "voice")
		if sourceErr == nil && hasRecordingHeader(source.path, "#!AMR\n") {
			if source.size <= weComVoiceMaxSize {
				return source, nil
			}
			source.mediaType = "file"
			source.note = "AMR-NB 录音超过 2 MiB，已按文件发送"
			return source, nil
		}
		fallbackNote = "原始 AMR-NB 录音不可用，已发送 MP3 文件"
	case "AMR-WB":
		source, sourceErr := weComFilePlan(attachment.SourcePath, "", "file")
		if sourceErr == nil && hasRecordingHeader(source.path, "#!AMR-WB\n") {
			source.note = "企业微信语音仅支持 AMR-NB，AMR-WB 录音已按文件发送"
			return source, nil
		}
		fallbackNote = "原始 AMR-WB 录音不可用，已发送 MP3 文件"
	default:
		codec := strings.TrimSpace(attachment.Codec)
		if codec == "" {
			codec = "当前格式"
		}
		fallbackNote = "企业微信语音仅支持 AMR-NB，" + codec + " 录音已按文件发送"
	}
	mainPlan, err := weComFilePlan(attachment.Path, attachment.Recording, "file")
	if err != nil {
		return weComMediaPlan{}, err
	}
	mainPlan.note = fallbackNote
	return mainPlan, nil
}

func weComFilePlan(path, filename, mediaType string) (weComMediaPlan, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return weComMediaPlan{}, errors.New("录音附件缺少服务端文件路径")
	}
	info, err := os.Stat(path)
	if err != nil {
		return weComMediaPlan{}, fmt.Errorf("读取录音文件信息失败: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return weComMediaPlan{}, errors.New("录音附件不是非空普通文件")
	}
	if info.Size() > weComFileMaxSize {
		return weComMediaPlan{}, fmt.Errorf("录音文件大小 %d 超过企业微信 20 MiB 限制", info.Size())
	}
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = filepath.Base(path)
	}
	return weComMediaPlan{path: path, filename: filename, mediaType: mediaType, size: info.Size()}, nil
}

func hasRecordingHeader(path, expected string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	header := make([]byte, len(expected))
	if _, err := io.ReadFull(file, header); err != nil {
		return false
	}
	return string(header) == expected
}

func (w *WeComBotChannel) uploadMediaPlan(ctx context.Context, plan weComMediaPlan) (weComUploadedMedia, error) {
	file, err := os.Open(plan.path)
	if err != nil {
		return weComUploadedMedia{}, fmt.Errorf("打开企业微信媒体文件失败: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return weComUploadedMedia{}, fmt.Errorf("读取企业微信媒体文件信息失败: %w", err)
	}
	if info.Size() != plan.size {
		return weComUploadedMedia{}, errors.New("企业微信媒体文件在发送前发生变化")
	}
	digest, err := hashWeComMedia(file, plan.size)
	if err != nil {
		return weComUploadedMedia{}, err
	}
	totalChunks := int((plan.size + weComUploadChunkSize - 1) / weComUploadChunkSize)
	if totalChunks > weComMaxUploadChunks {
		return weComUploadedMedia{}, fmt.Errorf("企业微信媒体分片数 %d 超过限制 %d", totalChunks, weComMaxUploadChunks)
	}
	uploadID, err := w.initializeMediaUpload(ctx, plan, totalChunks, digest)
	if err != nil {
		return weComUploadedMedia{}, err
	}
	if err := w.uploadMediaChunks(ctx, file, uploadID, plan.size, totalChunks); err != nil {
		return weComUploadedMedia{}, err
	}
	return w.finishMediaUpload(ctx, uploadID, plan.mediaType)
}

func hashWeComMedia(file *os.File, expectedSize int64) (string, error) {
	hash := md5.New()
	if _, err := io.CopyN(hash, file, expectedSize); err != nil {
		return "", fmt.Errorf("计算企业微信媒体 MD5 失败: %w", err)
	}
	extra := make([]byte, 1)
	if count, err := file.Read(extra); err != io.EOF || count != 0 {
		return "", errors.New("企业微信媒体文件在发送前发生变化")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("重置企业微信媒体文件失败: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (w *WeComBotChannel) initializeMediaUpload(
	ctx context.Context,
	plan weComMediaPlan,
	totalChunks int,
	digest string,
) (string, error) {
	frame, err := w.sendRequest(ctx, weComCommandUploadInit, "", map[string]any{
		"type": plan.mediaType, "filename": plan.filename, "total_size": plan.size,
		"total_chunks": totalChunks, "md5": digest,
	})
	if err != nil {
		return "", fmt.Errorf("企业微信媒体初始化失败: %w", err)
	}
	var body struct {
		UploadID string `json:"upload_id"`
	}
	if err := json.Unmarshal(frame.Body, &body); err != nil || strings.TrimSpace(body.UploadID) == "" {
		return "", errors.New("企业微信媒体初始化响应缺少 upload_id")
	}
	return strings.TrimSpace(body.UploadID), nil
}

func (w *WeComBotChannel) uploadMediaChunks(
	ctx context.Context,
	file *os.File,
	uploadID string,
	totalSize int64,
	totalChunks int,
) error {
	buffer := make([]byte, weComUploadChunkSize)
	remaining := totalSize
	for index := 0; index < totalChunks; index++ {
		chunkSize := min(int64(len(buffer)), remaining)
		if _, err := io.ReadFull(file, buffer[:chunkSize]); err != nil {
			return fmt.Errorf("读取企业微信媒体分片 %d 失败: %w", index, err)
		}
		_, err := w.sendRequest(ctx, weComCommandUploadChunk, "", map[string]any{
			"upload_id": uploadID, "chunk_index": index,
			"base64_data": base64.StdEncoding.EncodeToString(buffer[:chunkSize]),
		})
		if err != nil {
			return fmt.Errorf("企业微信媒体分片 %d 上传失败: %w", index, err)
		}
		remaining -= chunkSize
	}
	if remaining != 0 {
		return errors.New("企业微信媒体分片大小与文件不一致")
	}
	return nil
}

func (w *WeComBotChannel) finishMediaUpload(
	ctx context.Context,
	uploadID string,
	fallbackType string,
) (weComUploadedMedia, error) {
	frame, err := w.sendRequest(ctx, weComCommandUploadFinish, "", map[string]any{"upload_id": uploadID})
	if err != nil {
		return weComUploadedMedia{}, fmt.Errorf("企业微信媒体完成上传失败: %w", err)
	}
	var body struct {
		Type    string `json:"type"`
		MediaID string `json:"media_id"`
	}
	if err := json.Unmarshal(frame.Body, &body); err != nil || strings.TrimSpace(body.MediaID) == "" {
		return weComUploadedMedia{}, errors.New("企业微信媒体完成响应缺少 media_id")
	}
	mediaType := strings.TrimSpace(body.Type)
	if mediaType == "" {
		mediaType = fallbackType
	}
	if mediaType != "voice" && mediaType != "file" {
		return weComUploadedMedia{}, fmt.Errorf("企业微信媒体完成响应返回未知类型: %q", mediaType)
	}
	return weComUploadedMedia{mediaType: mediaType, mediaID: strings.TrimSpace(body.MediaID)}, nil
}

func (w *WeComBotChannel) sendUploadedMedia(
	ctx context.Context,
	target string,
	requestID string,
	media weComUploadedMedia,
) error {
	command := weComCommandSend
	body := map[string]any{
		"msgtype":       media.mediaType,
		media.mediaType: map[string]string{"media_id": media.mediaID},
	}
	if strings.TrimSpace(requestID) == "" {
		body["chatid"] = strings.TrimSpace(target)
	} else {
		command = weComCommandRespond
	}
	_, err := w.sendRequest(ctx, command, strings.TrimSpace(requestID), body)
	return err
}
