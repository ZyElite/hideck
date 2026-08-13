package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/config"
)

func TestServerRunsHTTPAndTrustedHTTPSAndExposesOnlyCACertificate(t *testing.T) {
	server := New(&config.Config{
		Server: config.ServerConfig{
			Port: "127.0.0.1:0", HTTPSPort: "127.0.0.1:0", TLSDataDir: t.TempDir(),
		},
		Web: config.WebConfig{Username: "admin", Password: "secret"},
	}, nil, nil, nil, nil, nil, "config.yaml")
	runError := make(chan error, 1)
	go func() { runError <- server.Run() }()
	httpAddress, httpsAddress := waitServerAddresses(t, server)

	response, err := http.Get("http://" + httpAddress + "/ping")
	if err != nil {
		t.Fatal(err)
	}
	assertPingResponse(t, response)
	caPEM, err := os.ReadFile(server.phoneCACertificate)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("generated CA certificate could not be loaded")
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: roots, MinVersion: tls.VersionTLS12,
	}}}
	response, err = client.Get("https://" + httpsAddress + "/ping")
	if err != nil {
		t.Fatal(err)
	}
	if response.TLS == nil || response.TLS.Version < tls.VersionTLS12 {
		t.Fatalf("TLS state = %+v", response.TLS)
	}
	assertPingResponse(t, response)

	response, err = http.Get("http://" + httpAddress + "/api/phone/ca.crt")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "BEGIN CERTIFICATE") ||
		strings.Contains(string(body), "PRIVATE KEY") {
		t.Fatalf("CA response status=%d body=%q", response.StatusCode, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-runError:
		if err != nil {
			t.Fatalf("Run returned after shutdown: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Run did not return after dual-server shutdown")
	}
}

func TestServerReturnsHTTPSBindFailureAndClosesHTTPListener(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	server := New(&config.Config{
		Server: config.ServerConfig{
			Port: "127.0.0.1:0", HTTPSPort: occupied.Addr().String(), TLSDataDir: t.TempDir(),
		},
		Web: config.WebConfig{Username: "admin", Password: "secret"},
	}, nil, nil, nil, nil, nil, "config.yaml")
	err = server.Run()
	if err == nil || !strings.Contains(err.Error(), "监听 HTTPS") {
		t.Fatalf("Run error = %v", err)
	}
	server.httpSrvMu.Lock()
	defer server.httpSrvMu.Unlock()
	if server.httpSrv != nil || server.httpsSrv != nil {
		t.Fatal("failed startup published partially running servers")
	}
}

func waitServerAddresses(t *testing.T, server *Server) (string, string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		server.httpSrvMu.Lock()
		httpServer, httpsServer := server.httpSrv, server.httpsSrv
		server.httpSrvMu.Unlock()
		if httpServer != nil && httpsServer != nil {
			return httpServer.Addr, httpsServer.Addr
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("HTTP and HTTPS servers did not start")
	return "", ""
}

func assertPingResponse(t *testing.T, response *http.Response) {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("ping status = %d", response.StatusCode)
	}
}
