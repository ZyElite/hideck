package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/db"
	"github.com/yibaiba/hideck/internal/device"
)

func TestSMSThreadReturnsReconciledMultipartMessage(t *testing.T) {
	if err := db.Init(filepath.Join(t.TempDir(), "sms-inbox.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB = nil })
	const imsi = "IMSI-API-FRAGMENT"
	if err := db.DB.Create(&db.SIMCard{ICCID: "ICCID-API-FRAGMENT", IMSI: imsi}).Error; err != nil {
		t.Fatal(err)
	}
	input := db.ReceivedMultipartSMS{
		IMSI: imsi, Sender: "giffgaff", Recipient: "+447840844894",
		LocalPhone: "+447840844894", FragmentSessionKey: "api-fragment-session",
		Content: "[incomplete 1/2 missing=2] first", Timestamp: time.Now(), Incomplete: true,
	}
	if _, err := db.SaveReceivedMultipartSMS(input); err != nil {
		t.Fatal(err)
	}
	input.Content, input.Incomplete = "complete inbox message", false
	if _, err := db.SaveReceivedMultipartSMS(input); err != nil {
		t.Fatal(err)
	}

	pool := device.NewPool(&config.Config{})
	t.Cleanup(func() { _ = pool.Shutdown() })
	server := &Server{pool: pool}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/sms/thread", server.handleGetSMSThread)
	query := url.Values{"device_id": {"all"}, "imsi": {imsi}, "peer": {"giffgaff"}}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sms/thread?"+query.Encode(), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var messages []SMSWithDevice
	if err := json.Unmarshal(recorder.Body.Bytes(), &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Content != "complete inbox message" || messages[0].Incomplete {
		t.Fatalf("thread messages=%+v", messages)
	}
	if strings.Contains(recorder.Body.String(), "fragment_session_key") || strings.Contains(recorder.Body.String(), "[incomplete") {
		t.Fatalf("thread leaked internal or degraded state: %s", recorder.Body.String())
	}
}
