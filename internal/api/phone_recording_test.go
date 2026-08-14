package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/config"
)

func TestPhoneRecordingSupportsAuthenticatedByteRanges(t *testing.T) {
	directory := t.TempDir()
	name := "call_dev_mixed.mp3"
	content := []byte("0123456789")
	if err := os.WriteFile(filepath.Join(directory, name), content, 0o644); err != nil {
		t.Fatal(err)
	}
	server := &Server{
		auth:                    config.WebConfig{Username: "admin", Password: "secret"},
		voiceRecordingDirectory: directory,
	}
	router := gin.New()
	api := router.Group("/api")
	api.Use(server.authMiddleware())
	api.GET("/phone/recordings/:recording", server.handleCommandRecording)

	token := testSessionToken(t, "secret", time.Now().Add(time.Hour))
	request := httptest.NewRequest(http.MethodGet, "/api/phone/recordings/"+name, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Range", "bytes=2-5")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusPartialContent || response.Body.String() != "2345" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("Content-Range=%q", got)
	}
}
