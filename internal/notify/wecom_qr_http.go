package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (s *WeComQRService) getJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "HiDeck/1.0")
	response, err := s.client.Do(request)
	if err != nil {
		return redactWeComTransportError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("企业微信扫码服务返回 HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, weComQRResponseLimit))
	if err := decoder.Decode(target); err != nil {
		return errors.New("企业微信扫码服务返回无效 JSON")
	}
	return nil
}

func appendWeComQRQuery(rawURL, key, value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("企业微信扫码服务地址无效")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHTTP(parsed)) {
		return "", errors.New("企业微信扫码服务地址必须使用 HTTPS")
	}
	query := parsed.Query()
	query.Set(key, value)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func validateWeComQRDisplayURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("企业微信二维码地址无效")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHTTP(parsed)) {
		return "", errors.New("企业微信二维码地址必须使用 HTTPS")
	}
	return parsed.String(), nil
}
