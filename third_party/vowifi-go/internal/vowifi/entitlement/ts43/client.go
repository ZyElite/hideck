package ts43

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
)

func DoJSONGzipRequest(
	ctx context.Context,
	client HTTPClient,
	url string,
	payload interface{},
	headers []HeaderPair,
) (*HTTPResponse, error) {
	if client == nil {
		return nil, errors.New("ts43 HTTP client is required")
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	body, err := encodeGzipJSON(payload)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(&HTTPRequest{
		Method: "POST", URL: url,
		Headers: append([]HeaderPair(nil), headers...),
		Body:    body,
	})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("ts43 HTTP client returned nil response")
	}
	decoded, err := DecodeGzipBodyIfPresent(response.Body)
	if err != nil {
		return nil, err
	}
	response.Body = decoded
	return response, nil
}

func encodeGzipJSON(payload interface{}) ([]byte, error) {
	plain, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var compressed bytes.Buffer
	writer, err := gzip.NewWriterLevel(&compressed, gzip.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(plain); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return compressed.Bytes(), nil
}

// DecodeGzipBodyIfPresent follows v1.5.5: a non-gzip body is returned intact.
func DecodeGzipBodyIfPresent(body []byte) ([]byte, error) {
	if len(body) == 0 {
		return nil, nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return append([]byte(nil), body...), nil
	}
	defer reader.Close()
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return append([]byte(nil), body...), nil
	}
	return decoded, nil
}
