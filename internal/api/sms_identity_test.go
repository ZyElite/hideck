package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/db"
	"github.com/iniwex5/vohive/internal/device"
)

func TestSMSContactsUsesDatabaseBindingWhenWorkerIsOffline(t *testing.T) {
	previousDB := db.DB
	if err := db.Init(filepath.Join(t.TempDir(), "offline-sms.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB = previousDB })

	iccid := "8944000000000000404"
	if err := db.DB.Create(&db.Device{IMEI: "imei-offline", Alias: "offline-1", CurrentICCID: &iccid}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&db.SIMCard{ICCID: iccid, IMSI: "imsi-offline"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSMSForIdentity(db.SMSRecord{
		Identity: db.SMSIdentity{ICCID: iccid, IMSI: "imsi-offline"},
		Sender:   "+10086", Content: "offline message", Type: 1, Status: 0, Timestamp: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	pool := device.NewPool(&config.Config{})
	t.Cleanup(func() { _ = pool.Shutdown() })
	server := &Server{pool: pool}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/sms/contacts", server.handleGetSMSContacts)
	query := url.Values{"device_id": {"offline-1"}}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sms/contacts?"+query.Encode(), nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var contacts []SMSContactWithDevice
	if err := json.Unmarshal(recorder.Body.Bytes(), &contacts); err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 1 || contacts[0].ICCID != iccid || contacts[0].LastContent != "offline message" {
		t.Fatalf("contacts=%+v", contacts)
	}
}

func TestResolveSMSICCIDRejectsAmbiguousIMSIQuery(t *testing.T) {
	previousDB := db.DB
	if err := db.Init(filepath.Join(t.TempDir(), "ambiguous-sms.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB = previousDB })
	for _, iccid := range []string{"ICC_API_A", "ICC_API_B"} {
		if err := db.DB.Create(&db.SIMCard{ICCID: iccid, IMSI: "IMSI_API_SHARED"}).Error; err != nil {
			t.Fatal(err)
		}
	}

	server := &Server{}
	_, status, _ := server.resolveSMSICCID("all", "IMSI_API_SHARED")

	if status != http.StatusConflict {
		t.Fatalf("status=%d, want %d", status, http.StatusConflict)
	}
}
