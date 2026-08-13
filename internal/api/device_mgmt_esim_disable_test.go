package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/device"
	"github.com/iniwex5/vohive/internal/esim"
)

func newEsimDisableTestServer(t *testing.T) *Server {
	t.Helper()
	mgr := newTestEsimManager()
	pool := device.NewPool(&config.Config{})
	setNestedPrivateField(t, pool, []string{"workers"}, map[string]*device.Worker{
		"dev-esim": {ID: "dev-esim", EsimMgr: mgr},
	})
	return &Server{pool: pool}
}

func performEsimDisableRequest(server *Server, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "device_id", Value: "dev-esim"}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/devices/dev-esim/esim/actions/disable", bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	server.handleEsimDisableProfile(ctx)
	return recorder
}

func TestHandleEsimDisableProfileSubmitsLPAOperation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldExec := esimDisableExec
	defer func() { esimDisableExec = oldExec }()
	server := newEsimDisableTestServer(t)

	esimDisableExec = func(_ func(context.Context, string, string) error, operation esimDisableOperation) error {
		if operation.ICCID != "8986001234567890123" || operation.AIDHex != "A000" {
			t.Fatalf("operation=%+v want trimmed ICCID and AID", operation)
		}
		if operation.Context == nil {
			t.Fatal("operation context is nil")
		}
		return nil
	}

	recorder := performEsimDisableRequest(server, `{"iccid":" 8986001234567890123 ","aid_hex":" A000 "}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if body := recorder.Body.String(); !containsAll(body, `"status":"ok"`, `"message":"eSIM Profile 停用指令已提交`) {
		t.Fatalf("body=%q want submitted response", body)
	}
}

func TestHandleEsimDisableProfileMapsBusyResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldExec := esimDisableExec
	defer func() { esimDisableExec = oldExec }()
	server := newEsimDisableTestServer(t)

	esimDisableExec = func(_ func(context.Context, string, string) error, _ esimDisableOperation) error {
		return esim.ErrOperationInProgress
	}
	recorder := performEsimDisableRequest(server, `{"iccid":"8986001234567890123","aid_hex":"A000"}`)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	if body := recorder.Body.String(); !containsAll(body, `"busy":true`, `"code":"ESIM_BUSY"`, `"reason":"disable_profile"`) {
		t.Fatalf("body=%q want busy response", body)
	}
}

func TestHandleEsimDisableProfileMapsCardResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldExec := esimDisableExec
	defer func() { esimDisableExec = oldExec }()

	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid ICCID",
			err:        esim.NewDisableProfileError(esim.DisableProfileErrorInvalidICCID, "bad iccid", nil),
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_ICCID",
		},
		{
			name:       "profile missing",
			err:        esim.NewDisableProfileError(esim.DisableProfileErrorProfileNotFound, "missing", nil),
			wantStatus: http.StatusNotFound,
			wantCode:   "PROFILE_NOT_FOUND",
		},
		{
			name:       "already disabled",
			err:        esim.NewDisableProfileError(esim.DisableProfileErrorProfileNotEnabled, "disabled", nil),
			wantStatus: http.StatusConflict,
			wantCode:   "PROFILE_NOT_ENABLED",
		},
		{
			name:       "policy rejected",
			err:        esim.NewDisableProfileError(esim.DisableProfileErrorDisallowedByPolicy, "policy", nil),
			wantStatus: http.StatusConflict,
			wantCode:   "DISALLOWED_BY_POLICY",
		},
		{
			name:       "SIM toolkit busy",
			err:        esim.NewDisableProfileError(esim.DisableProfileErrorCATBusy, "cat busy", nil),
			wantStatus: http.StatusConflict,
			wantCode:   "CAT_BUSY",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newEsimDisableTestServer(t)
			esimDisableExec = func(_ func(context.Context, string, string) error, _ esimDisableOperation) error {
				return tc.err
			}
			recorder := performEsimDisableRequest(server, `{"iccid":"8986001234567890123","aid_hex":"A000"}`)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			if body := recorder.Body.String(); !containsAll(body, `"code":"`+tc.wantCode+`"`) {
				t.Fatalf("body=%q want code=%s", body, tc.wantCode)
			}
			if tc.wantCode == "CAT_BUSY" {
				if body := recorder.Body.String(); !containsAll(body, `"busy":true`, `"retryAfterMs":1200`) {
					t.Fatalf("body=%q want retryable CAT busy response", body)
				}
			}
		})
	}
}

func TestHandleEsimDisableProfileRejectsEmptyICCID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := newEsimDisableTestServer(t)
	recorder := performEsimDisableRequest(server, `{"iccid":"   ","aid_hex":"A000"}`)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestHandleEsimDisableProfileRequiresManager(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{pool: device.NewPool(&config.Config{})}
	recorder := performEsimDisableRequest(server, `{"iccid":"8986001234567890123","aid_hex":"A000"}`)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}

func TestHandleEsimRenameProfileRejectsNicknameOver64Bytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := newEsimDisableTestServer(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{
		{Key: "device_id", Value: "dev-esim"},
		{Key: "iccid", Value: "8986001234567890123"},
	}
	ctx.Request = httptest.NewRequest(
		http.MethodPatch,
		"/devices/dev-esim/esim/profiles/8986001234567890123",
		bytes.NewBufferString(`{"name":"`+string(bytes.Repeat([]byte{'a'}, 65))+`","aid_hex":"A000"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	server.handleEsimRenameProfile(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if body := recorder.Body.String(); !containsAll(body, `64`) {
		t.Fatalf("body=%q want nickname length error", body)
	}
}

func TestEsimDisableRouteUsesProductionHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldExec := esimDisableExec
	defer func() { esimDisableExec = oldExec }()
	server := newEsimDisableTestServer(t)
	server.auth = config.WebConfig{Username: "admin", Password: "secret"}
	called := false
	esimDisableExec = func(_ func(context.Context, string, string) error, operation esimDisableOperation) error {
		called = true
		if operation.ICCID != "8986001234567890123" {
			t.Fatalf("ICCID=%q", operation.ICCID)
		}
		return nil
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/devices/dev-esim/esim/actions/disable",
		bytes.NewBufferString(`{"iccid":"8986001234567890123","aid_hex":"A000"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testSessionToken(t, "secret", time.Now().Add(time.Hour)))
	recorder := httptest.NewRecorder()
	server.newRouter().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !called {
		t.Fatal("disable operation was not called through production route")
	}
}
