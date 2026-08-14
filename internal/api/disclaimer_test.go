package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type disclaimerStoreStub struct {
	acceptedAt time.Time
	accepted   bool
	statusErr  error
	acceptErr  error
	version    string
}

func (stub *disclaimerStoreStub) Status(_ context.Context, version string) (time.Time, bool, error) {
	stub.version = version
	return stub.acceptedAt, stub.accepted, stub.statusErr
}

func (stub *disclaimerStoreStub) Accept(
	_ context.Context,
	version string,
	acceptedAt time.Time,
) (time.Time, error) {
	stub.version = version
	stub.acceptedAt = acceptedAt
	stub.accepted = stub.acceptErr == nil
	return acceptedAt, stub.acceptErr
}

func TestDisclaimerStatusReturnsDatabaseAcceptance(t *testing.T) {
	acceptedAt := time.Date(2026, time.August, 14, 4, 0, 0, 0, time.UTC)
	store := &disclaimerStoreStub{accepted: true, acceptedAt: acceptedAt}
	server := &Server{disclaimerAcceptances: store}
	recorder := requestDisclaimer(t, server, http.MethodGet, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload disclaimerStatusPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Accepted || payload.AcceptedAt == nil || !payload.AcceptedAt.Equal(acceptedAt) {
		t.Fatalf("payload=%+v", payload)
	}
	if store.version != disclaimerVersion {
		t.Fatalf("version=%q", store.version)
	}
}

func TestAcceptDisclaimerRequiresExactConfirmationAndPersists(t *testing.T) {
	store := &disclaimerStoreStub{}
	server := &Server{disclaimerAcceptances: store}

	invalid := requestDisclaimer(t, server, http.MethodPut, map[string]string{"confirmation": "同意"})
	if invalid.Code != http.StatusBadRequest || store.accepted {
		t.Fatalf("invalid status=%d accepted=%v", invalid.Code, store.accepted)
	}
	valid := requestDisclaimer(t, server, http.MethodPut, map[string]string{
		"confirmation": disclaimerConfirmationText,
	})
	if valid.Code != http.StatusOK || !store.accepted || store.acceptedAt.IsZero() {
		t.Fatalf("valid status=%d accepted=%v acceptedAt=%v", valid.Code, store.accepted, store.acceptedAt)
	}
}

func TestDisclaimerHandlersExposeDatabaseFailures(t *testing.T) {
	store := &disclaimerStoreStub{statusErr: errors.New("database offline")}
	server := &Server{disclaimerAcceptances: store}
	recorder := requestDisclaimer(t, server, http.MethodGet, nil)
	if recorder.Code != http.StatusInternalServerError || !bytes.Contains(recorder.Body.Bytes(), []byte("database offline")) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func requestDisclaimer(t *testing.T, server *Server, method string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, "/api/settings/disclaimer", bytes.NewReader(data))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if method == http.MethodGet {
		server.handleGetDisclaimerStatus(ctx)
	} else {
		server.handleAcceptDisclaimer(ctx)
	}
	return recorder
}
