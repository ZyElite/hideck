package rest

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientSendMediaUploadsSmallFileInlineAndSendsRichMedia(t *testing.T) {
	content := []byte("abcdefg")
	path := filepath.Join(t.TempDir(), "call.mp3")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	capture := &mediaProtocolCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/users/user-1/files":
			_ = json.NewDecoder(r.Body).Decode(&capture.upload)
			_, _ = w.Write([]byte(`{"file_info":"file-token"}`))
		case "/v2/users/user-1/messages":
			_ = json.NewDecoder(r.Body).Decode(&capture.message)
			_, _ = w.Write([]byte(`{"id":"media-message-1","timestamp":"2026-08-14T12:00:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := New(Config{
		BaseURL: server.URL, Client: server.Client(), Tokens: &fakeTokens{current: "token-1"},
	})

	result, err := client.SendMedia(context.Background(), MediaRequest{
		RecipientKind: "c2c", RecipientID: "user-1", FileType: MediaTypeVoice,
		Path: path, FileName: "recording.mp3", Content: "呼叫完成",
		Reply: &ReplyRequest{MessageID: "msg-1", Sequence: 2},
	})
	if err != nil {
		t.Fatalf("SendMedia() error = %v", err)
	}
	if result.ID != "media-message-1" {
		t.Fatalf("result ID = %q", result.ID)
	}
	if int(capture.upload["file_type"].(float64)) != MediaTypeVoice ||
		capture.upload["srv_send_msg"] != false ||
		capture.upload["file_data"] != base64.StdEncoding.EncodeToString(content) {
		t.Fatalf("upload payload = %#v", capture.upload)
	}
	assertRichMediaMessage(t, capture.message)
	if capture.prepare != nil || len(capture.parts) != 0 {
		t.Fatalf("small media unexpectedly used chunked upload: %+v", capture)
	}
}

func TestClientSendMediaUsesChunkedUploadAtThreshold(t *testing.T) {
	content := bytes.Repeat([]byte("a"), int(mediaChunkedThreshold))
	path := filepath.Join(t.TempDir(), "call.mp3")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	capture := &mediaProtocolCapture{}
	server := newMediaProtocolServer(t, capture, len(content))
	defer server.Close()
	client := New(Config{
		BaseURL: server.URL, Client: server.Client(), Tokens: &fakeTokens{current: "token-1"},
	})

	_, err := client.SendMedia(context.Background(), MediaRequest{
		RecipientKind: "c2c", RecipientID: "user-1", FileType: MediaTypeVoice,
		Path: path, FileName: "recording.mp3", Content: "呼叫完成",
		Reply: &ReplyRequest{MessageID: "msg-1", Sequence: 2},
	})
	if err != nil {
		t.Fatalf("SendMedia() error = %v", err)
	}
	assertMediaPrepare(t, capture.prepare, content)
	if len(capture.parts) != 1 || !bytes.Equal(capture.parts[0], content) {
		t.Fatalf("uploaded part count = %d", len(capture.parts))
	}
	assertMediaFinishes(t, capture.finishes, [][]byte{content})
	assertRichMediaMessage(t, capture.message)
	if capture.presignedAuth != "" {
		t.Fatalf("presigned upload leaked Authorization header: %q", capture.presignedAuth)
	}
}

type mediaProtocolCapture struct {
	upload        map[string]any
	prepare       map[string]any
	parts         [][]byte
	finishes      []map[string]any
	message       map[string]any
	presignedAuth string
}

func newMediaProtocolServer(
	t *testing.T, capture *mediaProtocolCapture, blockSize int,
) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/cos/") {
			capture.presignedAuth = r.Header.Get("Authorization")
			part, _ := io.ReadAll(r.Body)
			capture.parts = append(capture.parts, part)
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Header.Get("Authorization") != "QQBot token-1" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v2/users/user-1/upload_prepare":
			_ = json.NewDecoder(r.Body).Decode(&capture.prepare)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"upload_id": "upload-1", "block_size": blockSize,
				"parts": []map[string]any{
					{"part_index": 1, "presigned_url": server.URL + "/cos/1"},
				},
			}})
		case "/v2/users/user-1/upload_part_finish":
			var finish map[string]any
			_ = json.NewDecoder(r.Body).Decode(&finish)
			capture.finishes = append(capture.finishes, finish)
			_, _ = w.Write([]byte(`{}`))
		case "/v2/users/user-1/files":
			var complete map[string]any
			_ = json.NewDecoder(r.Body).Decode(&complete)
			if complete["upload_id"] != "upload-1" {
				t.Errorf("complete payload = %#v", complete)
			}
			_, _ = w.Write([]byte(`{"file_info":"file-token"}`))
		case "/v2/users/user-1/messages":
			_ = json.NewDecoder(r.Body).Decode(&capture.message)
			_, _ = w.Write([]byte(`{"id":"media-message-1","timestamp":"2026-08-14T12:00:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}

func assertMediaPrepare(t *testing.T, payload map[string]any, content []byte) {
	t.Helper()
	fullMD5 := md5.Sum(content)
	fullSHA1 := sha1.Sum(content)
	if int(payload["file_type"].(float64)) != MediaTypeVoice ||
		int(payload["file_size"].(float64)) != len(content) || payload["file_name"] != "recording.mp3" ||
		payload["md5"] != hex.EncodeToString(fullMD5[:]) || payload["md5_10m"] != hex.EncodeToString(fullMD5[:]) ||
		payload["sha1"] != hex.EncodeToString(fullSHA1[:]) {
		t.Fatalf("prepare payload = %#v", payload)
	}
}

func assertMediaFinishes(t *testing.T, finishes []map[string]any, parts [][]byte) {
	t.Helper()
	if len(finishes) != len(parts) {
		t.Fatalf("finish count = %d", len(finishes))
	}
	for index, part := range parts {
		digest := md5.Sum(part)
		finish := finishes[index]
		if int(finish["part_index"].(float64)) != index+1 ||
			int(finish["block_size"].(float64)) != len(part) ||
			finish["md5"] != hex.EncodeToString(digest[:]) {
			t.Fatalf("finish %d = %#v", index+1, finish)
		}
	}
}

func assertRichMediaMessage(t *testing.T, payload map[string]any) {
	t.Helper()
	media, _ := payload["media"].(map[string]any)
	if int(payload["msg_type"].(float64)) != mediaMessage || media["file_info"] != "file-token" ||
		payload["content"] != "呼叫完成" || payload["msg_id"] != "msg-1" ||
		int(payload["msg_seq"].(float64)) != 2 {
		t.Fatalf("message payload = %#v", payload)
	}
}

func TestClientSendMediaStopsWhenPartUploadFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "call.mp3")
	if err := os.WriteFile(path, []byte{0}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, mediaChunkedThreshold); err != nil {
		t.Fatal(err)
	}
	messageCalls := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/users/user-1/upload_prepare":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"upload_id": "upload-1", "block_size": mediaChunkedThreshold,
				"parts": []map[string]any{{"part_index": 1, "url": server.URL + "/cos/1"}},
			})
		case r.URL.Path == "/cos/1":
			w.WriteHeader(http.StatusBadGateway)
		case strings.HasSuffix(r.URL.Path, "/messages"):
			messageCalls++
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL, Client: server.Client(), Tokens: &fakeTokens{current: "token"}})
	_, err := client.SendMedia(context.Background(), MediaRequest{
		RecipientKind: "c2c", RecipientID: "user-1", FileType: MediaTypeVoice, Path: path,
	})
	if err == nil || !strings.Contains(err.Error(), "上传 QQ 媒体分片 1 失败") {
		t.Fatalf("error = %v", err)
	}
	if messageCalls != 0 {
		t.Fatalf("message calls = %d", messageCalls)
	}
}

func TestClientSendMediaReportsInvalidJSONWithoutResponseBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "call.mp3")
	if err := os.WriteFile(path, []byte("voice"), 0o600); err != nil {
		t.Fatal(err)
	}
	const sensitiveBody = "<html>internal gateway detail</html>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("x-tps-trace-id", "trace-1")
		_, _ = w.Write([]byte(sensitiveBody))
	}))
	defer server.Close()
	client := New(Config{
		BaseURL: server.URL, Client: server.Client(), Tokens: &fakeTokens{current: "token"},
	})

	_, err := client.SendMedia(context.Background(), MediaRequest{
		RecipientKind: "c2c", RecipientID: "user-1", FileType: MediaTypeVoice, Path: path,
	})
	if err == nil || !strings.Contains(err.Error(), "http_status=200") ||
		!strings.Contains(err.Error(), `content_type="text/html"`) ||
		!strings.Contains(err.Error(), `trace_id="trace-1"`) {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), sensitiveBody) {
		t.Fatalf("error leaked response body: %v", err)
	}
}

func TestClientSendMediaRejectsMissingFile(t *testing.T) {
	client := New(Config{BaseURL: "https://api.example.com", Client: http.DefaultClient, Tokens: &fakeTokens{current: "token"}})
	_, err := client.SendMedia(context.Background(), MediaRequest{
		RecipientKind: "group", RecipientID: "group-1", FileType: MediaTypeVoice,
		Path: filepath.Join(t.TempDir(), "missing.mp3"),
	})
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v", err)
	}
}

func TestHashMediaFileUsesExactTenMegabyteWindow(t *testing.T) {
	content := bytes.Repeat([]byte("a"), int(mediaHashWindow)+1)
	path := filepath.Join(t.TempDir(), "large.mp3")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hashes, err := hashMediaFile(file, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	first := md5.Sum(content[:mediaHashWindow])
	full := md5.Sum(content)
	if hashes.MD510M != hex.EncodeToString(first[:]) || hashes.MD5 != hex.EncodeToString(full[:]) {
		t.Fatalf("hashes = %+v", hashes)
	}
}
