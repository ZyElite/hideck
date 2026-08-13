package api

import (
	"context"
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

type smsIdentityBackendStub struct {
	ussdDeviceBackendStub
	imsi string
}

func (s *smsIdentityBackendStub) GetIMSI(context.Context) (string, error) {
	return s.imsi, nil
}

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

func TestResolveSMSSendWorkerUsesICCIDWhenIMSIIsShared(t *testing.T) {
	previousDB := db.DB
	if err := db.Init(filepath.Join(t.TempDir(), "send-worker-sms.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB = previousDB })

	pool := device.NewPool(&config.Config{})
	t.Cleanup(func() { _ = pool.Shutdown() })
	workers := map[string]*device.Worker{
		"device-a": {ID: "device-a", Backend: &smsIdentityBackendStub{imsi: "IMSI_SHARED"}},
		"device-b": {ID: "device-b", Backend: &smsIdentityBackendStub{imsi: "IMSI_SHARED"}},
	}
	setNestedPrivateField(t, workers["device-a"], []string{"state", "Identity", "ICCID"}, "8944000000000000501F")
	setNestedPrivateField(t, workers["device-b"], []string{"state", "Identity", "ICCID"}, "8944000000000000502F")
	setNestedPrivateField(t, pool, []string{"workers"}, workers)
	server := &Server{pool: pool}

	worker, status, message := server.resolveSMSSendWorker("", "", "8944000000000000502")
	if status != 0 || worker == nil || worker.ID != "device-b" {
		t.Fatalf("worker=%v status=%d message=%q", worker, status, message)
	}
	if _, status, _ := server.resolveSMSSendWorker("", "IMSI_SHARED", ""); status != http.StatusConflict {
		t.Fatalf("shared IMSI status=%d, want %d", status, http.StatusConflict)
	}
}

func TestDeleteSMSThreadByICCIDDoesNotDeleteSharedIMSIThread(t *testing.T) {
	previousDB := db.DB
	if err := db.Init(filepath.Join(t.TempDir(), "delete-thread-sms.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB = previousDB })

	const (
		imsi   = "IMSI_DELETE_SHARED"
		iccidA = "8944000000000000601"
		iccidB = "8944000000000000602"
		peer   = "+10086"
	)
	for _, iccid := range []string{iccidA, iccidB} {
		if err := db.DB.Create(&db.SIMCard{ICCID: iccid, IMSI: imsi}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.SaveSMSForIdentity(db.SMSRecord{
			Identity: db.SMSIdentity{ICCID: iccid, IMSI: imsi}, Sender: peer,
			Content: iccid, Type: 1, Status: 0, Timestamp: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	server := &Server{}
	router := gin.New()
	router.DELETE("/sms/thread", server.handleDeleteSMSThread)
	query := url.Values{"iccid": {iccidA}, "peer": {peer}}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/sms/thread?"+query.Encode(), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var remaining int64
	if err := db.DB.Model(&db.SMS{}).Where("iccid = ? AND peer = ?", iccidB, peer).Count(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("remaining shared-IMSI thread messages=%d, want 1", remaining)
	}
}
