package rest

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	MediaTypeImage = 1
	MediaTypeVideo = 2
	MediaTypeVoice = 3
	MediaTypeFile  = 4
	mediaMessage   = 7
)

type MediaRequest struct {
	RecipientKind string
	RecipientID   string
	FileType      int
	Path          string
	FileName      string
	Content       string
	Reply         *ReplyRequest
}

type mediaPreparePart struct {
	Index       int    `json:"part_index"`
	Fallback    int    `json:"index"`
	URL         string `json:"presigned_url"`
	FallbackURL string `json:"url"`
	BlockSize   int64  `json:"block_size"`
}

type mediaPrepareData struct {
	UploadID  string             `json:"upload_id"`
	BlockSize int64              `json:"block_size"`
	Parts     []mediaPreparePart `json:"parts"`
	PartList  []mediaPreparePart `json:"part_list"`
}

type mediaPrepareResponse struct {
	mediaPrepareData
	Data *mediaPrepareData `json:"data"`
}

type mediaCompleteData struct {
	FileInfo string `json:"file_info"`
}

type mediaCompleteResponse struct {
	mediaCompleteData
	Data *mediaCompleteData `json:"data"`
}

func (c *Client) SendMedia(ctx context.Context, request MediaRequest) (SendResult, error) {
	request, err := normalizeMediaRequest(request)
	if err != nil {
		return SendResult{}, err
	}
	file, size, err := openMediaFile(request.Path)
	if err != nil {
		return SendResult{}, err
	}
	defer file.Close()
	hashes, err := hashMediaFile(file, size)
	if err != nil {
		return SendResult{}, fmt.Errorf("计算 QQ 媒体文件哈希失败: %w", err)
	}
	prepare, err := c.prepareMediaUpload(ctx, request, size, hashes)
	if err != nil {
		return SendResult{}, fmt.Errorf("准备 QQ 媒体上传失败: %w", err)
	}
	if err := c.uploadMediaParts(ctx, request, file, size, prepare); err != nil {
		return SendResult{}, err
	}
	fileInfo, err := c.completeMediaUpload(ctx, request, prepare.UploadID)
	if err != nil {
		return SendResult{}, fmt.Errorf("完成 QQ 媒体上传失败: %w", err)
	}
	return c.sendMediaMessage(ctx, request, fileInfo)
}

func normalizeMediaRequest(request MediaRequest) (MediaRequest, error) {
	request.RecipientKind = strings.TrimSpace(request.RecipientKind)
	request.RecipientID = strings.TrimSpace(request.RecipientID)
	request.Path = strings.TrimSpace(request.Path)
	request.FileName = strings.TrimSpace(request.FileName)
	if request.RecipientKind != "c2c" && request.RecipientKind != "group" {
		return MediaRequest{}, errors.New("recipient_kind 只支持 c2c 或 group")
	}
	if request.RecipientID == "" || request.Path == "" {
		return MediaRequest{}, errors.New("QQ 媒体 recipient_id 和 path 不能为空")
	}
	if request.FileType < MediaTypeImage || request.FileType > MediaTypeFile {
		return MediaRequest{}, errors.New("QQ 媒体 file_type 无效")
	}
	if request.FileName == "" {
		request.FileName = filepath.Base(request.Path)
	}
	return request, nil
}

func openMediaFile(path string) (*os.File, int64, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, 0, fmt.Errorf("打开 QQ 媒体文件失败: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, fmt.Errorf("读取 QQ 媒体文件信息失败: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		file.Close()
		return nil, 0, errors.New("QQ 媒体文件必须是非空普通文件")
	}
	return file, info.Size(), nil
}

func mediaEndpoint(baseURL string, request MediaRequest, suffix string) string {
	scope := "groups"
	if request.RecipientKind == "c2c" {
		scope = "users"
	}
	return fmt.Sprintf("%s/v2/%s/%s/%s", baseURL, scope, url.PathEscape(request.RecipientID), suffix)
}

func (response mediaPrepareResponse) payload() mediaPrepareData {
	if response.Data != nil {
		return *response.Data
	}
	return response.mediaPrepareData
}

func (response mediaCompleteResponse) fileInfo() string {
	if response.Data != nil {
		return strings.TrimSpace(response.Data.FileInfo)
	}
	return strings.TrimSpace(response.FileInfo)
}
