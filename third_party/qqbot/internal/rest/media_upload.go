package rest

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	mediaHashWindow = int64(10_002_432)
	mediaHashBuffer = 64 * 1024
)

type mediaHashes struct {
	MD5    string
	SHA1   string
	MD510M string
}

func (c *Client) uploadMediaFile(
	ctx context.Context, request MediaRequest, file *os.File, size int64,
) (string, error) {
	if size < mediaChunkedThreshold {
		return c.uploadInlineMedia(ctx, request, file, size)
	}
	hashes, err := hashMediaFile(file, size)
	if err != nil {
		return "", fmt.Errorf("计算 QQ 媒体文件哈希失败: %w", err)
	}
	prepare, err := c.prepareMediaUpload(ctx, request, size, hashes)
	if err != nil {
		return "", fmt.Errorf("准备 QQ 媒体上传失败: %w", err)
	}
	if err := c.uploadMediaParts(ctx, request, file, size, prepare); err != nil {
		return "", err
	}
	fileInfo, err := c.completeMediaUpload(ctx, request, prepare.UploadID)
	if err != nil {
		return "", fmt.Errorf("完成 QQ 媒体上传失败: %w", err)
	}
	return fileInfo, nil
}

func (c *Client) uploadInlineMedia(
	ctx context.Context, request MediaRequest, file *os.File, size int64,
) (string, error) {
	data, err := io.ReadAll(io.LimitReader(file, mediaChunkedThreshold))
	if err != nil {
		return "", fmt.Errorf("读取 QQ 媒体文件失败: %w", err)
	}
	if int64(len(data)) != size {
		return "", fmt.Errorf("文件读取长度为 %d，预期 %d", len(data), size)
	}
	payload := map[string]any{
		"file_type": request.FileType, "srv_send_msg": false,
		"file_data": base64.StdEncoding.EncodeToString(data),
	}
	if request.FileType == MediaTypeFile {
		payload["file_name"] = request.FileName
	}
	var response mediaCompleteResponse
	endpoint := mediaEndpoint(c.baseURL, request, "files")
	if err := c.doAuthenticatedJSON(ctx, endpoint, payload, &response); err != nil {
		return "", fmt.Errorf("上传 QQ 媒体失败: %w", err)
	}
	fileInfo := response.fileInfo()
	if fileInfo == "" {
		return "", errors.New("QQ 媒体上传响应缺少 file_info")
	}
	return fileInfo, nil
}

func hashMediaFile(file *os.File, size int64) (mediaHashes, error) {
	fullMD5 := md5.New()
	fullSHA1 := sha1.New()
	firstMD5 := md5.New()
	reader := io.NewSectionReader(file, 0, size)
	buffer := make([]byte, mediaHashBuffer)
	var read int64
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			chunk := buffer[:count]
			_, _ = fullMD5.Write(chunk)
			_, _ = fullSHA1.Write(chunk)
			remaining := mediaHashWindow - read
			if remaining > 0 {
				firstCount := min(int64(count), remaining)
				_, _ = firstMD5.Write(chunk[:firstCount])
			}
			read += int64(count)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return mediaHashes{}, err
		}
	}
	if read != size {
		return mediaHashes{}, fmt.Errorf("文件读取长度为 %d，预期 %d", read, size)
	}
	return mediaHashes{
		MD5: hex.EncodeToString(fullMD5.Sum(nil)), SHA1: hex.EncodeToString(fullSHA1.Sum(nil)),
		MD510M: hex.EncodeToString(firstMD5.Sum(nil)),
	}, nil
}

func (c *Client) prepareMediaUpload(
	ctx context.Context, request MediaRequest, size int64, hashes mediaHashes,
) (mediaPrepareData, error) {
	payload := map[string]any{
		"file_type": request.FileType, "file_name": request.FileName, "file_size": size,
		"md5": hashes.MD5, "sha1": hashes.SHA1, "md5_10m": hashes.MD510M,
	}
	var response mediaPrepareResponse
	endpoint := mediaEndpoint(c.baseURL, request, "upload_prepare")
	if err := c.doAuthenticatedJSON(ctx, endpoint, payload, &response); err != nil {
		return mediaPrepareData{}, err
	}
	prepare := response.payload()
	if err := validateMediaPrepare(prepare, size); err != nil {
		return mediaPrepareData{}, err
	}
	return prepare, nil
}

func validateMediaPrepare(prepare mediaPrepareData, fileSize int64) error {
	if prepare.UploadID == "" {
		return errors.New("upload_prepare 响应缺少 upload_id")
	}
	return validateMediaPrepareFields(prepare, fileSize)
}

func validateMediaPrepareFields(prepare mediaPrepareData, fileSize int64) error {
	if prepare.BlockSize <= 0 {
		return errors.New("upload_prepare 响应缺少有效 block_size")
	}
	parts := prepare.Parts
	if len(parts) == 0 {
		parts = prepare.PartList
	}
	expected := int((fileSize + prepare.BlockSize - 1) / prepare.BlockSize)
	if len(parts) != expected {
		return fmt.Errorf("upload_prepare 分片数量为 %d，预期 %d", len(parts), expected)
	}
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		index := mediaPartIndex(part)
		if index <= 0 || index > expected || mediaPartURL(part) == "" {
			return errors.New("upload_prepare 返回无效分片")
		}
		if _, exists := seen[index]; exists {
			return errors.New("upload_prepare 返回重复分片")
		}
		seen[index] = struct{}{}
	}
	return nil
}

func (c *Client) uploadMediaParts(
	ctx context.Context, request MediaRequest, file *os.File, size int64, prepare mediaPrepareData,
) error {
	parts := prepare.Parts
	if len(parts) == 0 {
		parts = prepare.PartList
	}
	for _, part := range parts {
		if err := c.uploadMediaPart(ctx, request, file, size, prepare, part); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) uploadMediaPart(
	ctx context.Context, request MediaRequest, file *os.File, size int64,
	prepare mediaPrepareData, part mediaPreparePart,
) error {
	index := mediaPartIndex(part)
	offset := int64(index-1) * prepare.BlockSize
	length := min(mediaPartSize(part, prepare.BlockSize), size-offset)
	data := make([]byte, length)
	if _, err := file.ReadAt(data, offset); err != nil {
		return fmt.Errorf("读取 QQ 媒体分片 %d 失败: %w", index, err)
	}
	digest := md5.Sum(data)
	if err := c.putMediaPart(ctx, mediaPartURL(part), data); err != nil {
		return fmt.Errorf("上传 QQ 媒体分片 %d 失败: %w", index, err)
	}
	payload := map[string]any{
		"upload_id": prepare.UploadID, "part_index": index,
		"block_size": length, "md5": hex.EncodeToString(digest[:]),
	}
	endpoint := mediaEndpoint(c.baseURL, request, "upload_part_finish")
	if err := c.doAuthenticatedJSON(ctx, endpoint, payload, nil); err != nil {
		return fmt.Errorf("确认 QQ 媒体分片 %d 失败: %w", index, err)
	}
	return nil
}

func mediaPartIndex(part mediaPreparePart) int {
	if part.Index > 0 {
		return part.Index
	}
	return part.Fallback
}

func mediaPartURL(part mediaPreparePart) string {
	if part.URL != "" {
		return part.URL
	}
	return part.FallbackURL
}

func mediaPartSize(part mediaPreparePart, fallback int64) int64 {
	if part.BlockSize > 0 {
		return part.BlockSize
	}
	return fallback
}

func (c *Client) completeMediaUpload(
	ctx context.Context, request MediaRequest, uploadID string,
) (string, error) {
	var response mediaCompleteResponse
	endpoint := mediaEndpoint(c.baseURL, request, "files")
	if err := c.doAuthenticatedJSON(ctx, endpoint, map[string]string{"upload_id": uploadID}, &response); err != nil {
		return "", err
	}
	fileInfo := response.fileInfo()
	if fileInfo == "" {
		return "", errors.New("QQ 媒体上传响应缺少 file_info")
	}
	return fileInfo, nil
}

func (c *Client) sendMediaMessage(
	ctx context.Context, request MediaRequest, fileInfo string,
) (SendResult, error) {
	payload := map[string]any{
		"msg_type": mediaMessage,
		"media":    map[string]string{"file_info": fileInfo},
	}
	if request.Content != "" {
		payload["content"] = request.Content
	}
	appendReplyPayload(payload, request.Reply)
	var response messageReply
	endpoint := mediaEndpoint(c.baseURL, request, "messages")
	if err := c.doAuthenticatedJSON(ctx, endpoint, payload, &response); err != nil {
		return SendResult{}, fmt.Errorf("发送 QQ 富媒体消息失败: %w", err)
	}
	return SendResult{ID: response.ID, At: parseMoment(response.Timestamp)}, nil
}

func appendReplyPayload(payload map[string]any, reply *ReplyRequest) {
	if reply == nil {
		return
	}
	if reply.MessageID != "" {
		payload["msg_id"] = reply.MessageID
	}
	if reply.Sequence > 0 {
		payload["msg_seq"] = reply.Sequence
	}
	if reply.EventID != "" {
		payload["event_id"] = reply.EventID
	}
}
