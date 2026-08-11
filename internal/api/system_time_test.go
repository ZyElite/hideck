package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/config"
)

type systemTimeProviderStub struct {
	info systemTimeInfo
}

func (s systemTimeProviderStub) Snapshot() systemTimeInfo { return s.info }

func unavailableFile(string) ([]byte, error) { return nil, errors.New("unavailable") }

func unavailableLink(string) (string, error) { return "", errors.New("unavailable") }

func TestSystemTimeProviderUsesEtcTimezone(t *testing.T) {
	localOnly := time.FixedZone("Local", 0)
	instant := time.Date(2026, 8, 11, 4, 30, 0, 0, localOnly)
	provider := osSystemTimeProvider{deps: systemTimeDependencies{
		now:      func() time.Time { return instant },
		getenv:   func(string) string { return "" },
		readFile: func(string) ([]byte, error) { return []byte("Asia/Shanghai\n"), nil },
		readlink: unavailableLink,
	}}

	got := provider.Snapshot()
	if got.Timezone != "Asia/Shanghai" || got.Source != "etc_timezone" || got.OffsetSeconds != 8*60*60 {
		t.Fatalf("snapshot=%+v", got)
	}
	if got.Now != "2026-08-11T12:30:00+08:00" {
		t.Fatalf("now=%q", got.Now)
	}
}

func TestSystemTimeProviderPrefersExplicitRuntimeLocation(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	provider := osSystemTimeProvider{deps: systemTimeDependencies{
		now:      func() time.Time { return time.Date(2026, 8, 11, 5, 30, 0, 0, london) },
		getenv:   func(string) string { return "Asia/Shanghai" },
		readFile: func(string) ([]byte, error) { return []byte("Asia/Tokyo"), nil },
		readlink: unavailableLink,
	}}

	got := provider.Snapshot()
	if got.Timezone != "Europe/London" || got.Source != "runtime_location" || got.OffsetSeconds != 3600 {
		t.Fatalf("snapshot=%+v", got)
	}
}

func TestSystemTimeProviderExposesFixedOffsetFallback(t *testing.T) {
	location := time.FixedZone("unknown-device-zone", 5*60*60+30*60)
	provider := osSystemTimeProvider{deps: systemTimeDependencies{
		now:      func() time.Time { return time.Date(2026, 8, 11, 9, 0, 0, 0, location) },
		getenv:   func(string) string { return "invalid-zone" },
		readFile: unavailableFile,
		readlink: unavailableLink,
	}}

	got := provider.Snapshot()
	if got.Timezone != "" || got.Source != "fixed_offset" || got.OffsetSeconds != 19800 {
		t.Fatalf("snapshot=%+v", got)
	}
	if _, err := time.Parse(time.RFC3339Nano, got.Now); err != nil {
		t.Fatalf("now is not RFC3339: %v", err)
	}
}

func TestReadLocaltimeZoneExtractsIANAName(t *testing.T) {
	got := readLocaltimeZone(func(string) (string, error) {
		return "../usr/share/zoneinfo/posix/Europe/London", nil
	})
	if got != "Europe/London" {
		t.Fatalf("zone=%q", got)
	}
}

func TestSystemTimeEndpointReturnsInjectedSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	want := systemTimeInfo{
		Now: "2026-08-11T12:30:00+08:00", Timezone: "Asia/Shanghai",
		OffsetSeconds: 28800, Source: "runtime_location",
	}
	s := &Server{
		auth:       config.WebConfig{Username: "admin", Password: "secret"},
		systemTime: systemTimeProviderStub{info: want},
	}
	router := s.newRouter()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/system/time", nil)
	request.Header.Set("Authorization", "Bearer "+testSessionToken(t, "secret", time.Now().Add(time.Hour)))
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var got systemTimeInfo
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
}
