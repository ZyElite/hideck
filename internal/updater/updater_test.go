package updater

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCheckerFindsLatestStableDockerTagAcrossPages(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `{"next":null,"results":[{"name":"latest"},{"name":"2.1.0-beta.1"},{"name":"2.1.0"}]}`)
			return
		}
		fmt.Fprint(w, `{"next":"/tags?page=2","results":[{"name":"2.0"},{"name":"2.0.0"}]}`)
	}))
	defer server.Close()

	checker := NewChecker(server.Client(), CheckerOptions{
		TagsURL:        server.URL + "/tags",
		CurrentVersion: "v2.0.0",
		IsDocker:       true,
	})
	info, err := checker.CheckUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckUpdate() error = %v", err)
	}
	if !info.HasUpdate || info.LatestVer != "2.1.0" || !info.IsDocker {
		t.Fatalf("CheckUpdate() = %+v, want Docker update 2.1.0", info)
	}
	if !strings.Contains(info.ReleaseNote, "拉取最新镜像") {
		t.Fatalf("ReleaseNote = %q, want Docker deployment instructions", info.ReleaseNote)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
}

func TestCheckerTreatsEquivalentVersionAliasesAsCurrent(t *testing.T) {
	server := newTagsServer(t, http.StatusOK, `{"next":null,"results":[{"name":"2.0"},{"name":"2.0.0"},{"name":"latest"}]}`)
	checker := NewChecker(server.Client(), CheckerOptions{
		TagsURL:        server.URL,
		CurrentVersion: "v2.0.0",
	})

	info, err := checker.CheckUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckUpdate() error = %v", err)
	}
	if info.HasUpdate {
		t.Fatalf("HasUpdate = true, want false: %+v", info)
	}
	if info.LatestVer != "2.0.0" {
		t.Fatalf("LatestVer = %q, want canonical three-part tag", info.LatestVer)
	}
}

func TestCheckerReportsInvalidCurrentVersionWithoutNetworkRequest(t *testing.T) {
	var requested atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requested.Store(true)
	}))
	defer server.Close()
	checker := NewChecker(server.Client(), CheckerOptions{
		TagsURL:        server.URL,
		CurrentVersion: "Unknown",
	})

	_, err := checker.CheckUpdate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "当前构建版本无效") {
		t.Fatalf("CheckUpdate() error = %v, want invalid current version", err)
	}
	if requested.Load() {
		t.Fatal("invalid current version should fail before requesting Docker Hub")
	}
}

func TestCheckerExposesDockerHubFailures(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantError  string
	}{
		{name: "http status", statusCode: http.StatusTooManyRequests, body: `{}`, wantError: "HTTP 429"},
		{name: "malformed json", statusCode: http.StatusOK, body: `{`, wantError: "解析 Docker Hub"},
		{name: "no stable tags", statusCode: http.StatusOK, body: `{"results":[{"name":"latest"}]}`, wantError: "未返回可用的稳定版本标签"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTagsServer(t, tt.statusCode, tt.body)
			checker := NewChecker(server.Client(), CheckerOptions{
				TagsURL:        server.URL,
				CurrentVersion: "2.0.0",
			})

			_, err := checker.CheckUpdate(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("CheckUpdate() error = %v, want containing %q", err, tt.wantError)
			}
		})
	}
}

func TestResolveNextPageRejectsUnexpectedService(t *testing.T) {
	current := mustParseURL(t, "https://hub.docker.com/v2/repositories/yibaiba/hideck/tags")
	_, err := resolveNextPage(current, "https://example.com/tags?page=2")
	if err == nil || !strings.Contains(err.Error(), "非预期服务") {
		t.Fatalf("resolveNextPage() error = %v, want unexpected service", err)
	}
}

func TestCompareStableVersions(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{left: "2.1.0", right: "2.0.9", want: 1},
		{left: "2.0", right: "2.0.0", want: 0},
		{left: "v1.9.9", right: "2.0.0", want: -1},
	}
	for _, tt := range tests {
		left, leftErr := parseStableVersion(tt.left)
		right, rightErr := parseStableVersion(tt.right)
		if leftErr != nil || rightErr != nil {
			t.Fatalf("parse versions %q/%q: %v/%v", tt.left, tt.right, leftErr, rightErr)
		}
		if got := compareVersions(left, right); got != tt.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", tt.left, tt.right, got, tt.want)
		}
	}
}

func newTagsServer(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	return parsed
}
