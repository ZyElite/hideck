package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/db"
)

func TestMarkSMSThreadReadRoutePersistsStateAndRequiresAuthentication(t *testing.T) {
	previousDB := db.DB
	if err := db.Init(filepath.Join(t.TempDir(), "sms-read-api.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB = previousDB })

	const (
		iccid = "ICC-READ-API"
		imsi  = "IMSI-READ-API"
		peer  = "+10086"
	)
	if err := db.DB.Create(&db.SIMCard{ICCID: iccid, IMSI: imsi}).Error; err != nil {
		t.Fatal(err)
	}
	for index, content := range []string{"seen", "newer"} {
		err := db.SaveSMSForIdentity(db.SMSRecord{
			Identity: db.SMSIdentity{ICCID: iccid, IMSI: imsi}, Sender: peer,
			Content: content, Type: 1, Status: 0, Timestamp: time.Now().Add(time.Duration(index) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var boundary db.SMS
	if err := db.DB.Where("iccid = ? AND peer = ? AND content = ?", iccid, peer, "seen").First(&boundary).Error; err != nil {
		t.Fatal(err)
	}

	router := smsReadTestRouter()
	path := "/api/sms/thread?" + url.Values{"iccid": {iccid}, "peer": {peer}}.Encode()
	unauthorized := performSMSReadRequest(router, path, "", boundary.ID)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	authorized := performSMSReadRequest(router, path, testSessionToken(t, "secret", time.Now().Add(time.Hour)), boundary.ID)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized status=%d body=%s", authorized.Code, authorized.Body.String())
	}
	var response struct {
		Marked      int64 `json:"marked"`
		UnreadCount int   `json:"unread_count"`
	}
	if err := json.Unmarshal(authorized.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Marked != 1 || response.UnreadCount != 1 {
		t.Fatalf("response=%+v want marked=1 unread=1", response)
	}
}

func smsReadTestRouter() http.Handler {
	server := &Server{auth: config.WebConfig{Username: "admin", Password: "secret"}}
	router := gin.New()
	api := router.Group("/api")
	api.Use(server.authMiddleware())
	api.PATCH("/sms/thread", server.handleMarkSMSThreadRead)
	return router
}

func performSMSReadRequest(router http.Handler, path, token string, throughID uint) *httptest.ResponseRecorder {
	body, _ := json.Marshal(markSMSThreadReadRequest{ThroughID: throughID})
	request := httptest.NewRequest(http.MethodPatch, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
