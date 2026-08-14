package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const mediaResponseLimit = 1 << 20

func (c *Client) doAuthenticatedJSON(ctx context.Context, endpoint string, payload, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.tokens.Token(ctx)
		if err != nil {
			return err
		}
		status, header, raw, err := c.postJSON(ctx, endpoint, token, body)
		if err != nil {
			return err
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			c.tokens.Invalidate()
			continue
		}
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			return decodeMediaAPIError(status, header, raw)
		}
		if target == nil || len(bytes.TrimSpace(raw)) == 0 {
			return nil
		}
		if err := json.Unmarshal(raw, target); err != nil {
			return errors.New("QQ 媒体接口返回无效 JSON")
		}
		return nil
	}
	return errors.New("QQ 媒体接口鉴权失败")
}

func (c *Client) postJSON(
	ctx context.Context, endpoint, token string, body []byte,
) (int, http.Header, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	request.Header.Set("Authorization", "QQBot "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return 0, nil, nil, redactMediaTransportError(err)
	}
	defer response.Body.Close()
	raw, err := readLimitedMediaResponse(response.Body)
	return response.StatusCode, response.Header.Clone(), raw, err
}

func readLimitedMediaResponse(reader io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, mediaResponseLimit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > mediaResponseLimit {
		return nil, errors.New("QQ 媒体接口响应过大")
	}
	return raw, nil
}

func decodeMediaAPIError(status int, header http.Header, raw []byte) error {
	apiErr := &apiError{StatusCode: status, TraceID: header.Get("x-tps-trace-id")}
	_ = json.Unmarshal(raw, apiErr)
	return apiErr
}

func (c *Client) putMediaPart(ctx context.Context, endpoint string, data []byte) error {
	if err := validatePresignedMediaURL(endpoint); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.ContentLength = int64(len(data))
	response, err := c.client.Do(request)
	if err != nil {
		return redactMediaTransportError(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("预签名上传返回 HTTP %d", response.StatusCode)
	}
	return nil
}

func validatePresignedMediaURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return errors.New("QQ 媒体预签名地址无效")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	host := parsed.Hostname()
	if parsed.Scheme == "http" && (host == "localhost" || isLoopbackIP(host)) {
		return nil
	}
	return errors.New("QQ 媒体预签名地址必须使用 HTTPS")
}

func isLoopbackIP(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func redactMediaTransportError(err error) error {
	var urlError *url.Error
	if errors.As(err, &urlError) && urlError.Err != nil {
		return urlError.Err
	}
	return err
}
