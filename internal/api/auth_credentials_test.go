package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func TestLoginReportsCredentialChangeRequirement(t *testing.T) {
	weakHash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	strongHash, err := bcrypt.GenerateFromPassword([]byte("CorrectHorse9"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		stored     string
		input      string
		source     config.WebPasswordSource
		wantChange bool
		wantMode   string
	}{
		{name: "plaintext config migrates", stored: "LongConfigPassword", input: "LongConfigPassword", source: config.WebPasswordSourceConfigFile, wantChange: true, wantMode: "config_file"},
		{name: "weak bcrypt changes", stored: string(weakHash), input: "admin123", source: config.WebPasswordSourceConfigFile, wantChange: true, wantMode: "config_file"},
		{name: "strong bcrypt accepted", stored: string(strongHash), input: "CorrectHorse9", source: config.WebPasswordSourceConfigFile, wantMode: "config_file"},
		{name: "weak environment changes externally", stored: "admin", input: "admin", source: config.WebPasswordSourceEnvironment, wantChange: true, wantMode: "environment"},
		{name: "strong environment accepted", stored: "correct-horse-battery", input: "correct-horse-battery", source: config.WebPasswordSourceEnvironment, wantMode: "environment"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := &Server{
				auth:          config.WebConfig{Username: "admin", Password: tc.stored, PasswordSource: tc.source},
				loginAttempts: make(map[string]loginAttempt),
			}
			response := loginForCredentialStatus(t, server, tc.input)
			if response.Credential.ChangeRequired != tc.wantChange {
				t.Fatalf("change_required=%v want %v", response.Credential.ChangeRequired, tc.wantChange)
			}
			if response.Credential.Management != tc.wantMode {
				t.Fatalf("management=%q want %q", response.Credential.Management, tc.wantMode)
			}
			if got := server.passwordStatus("").ChangeRequired; got != tc.wantChange {
				t.Fatalf("persisted change_required=%v want %v", got, tc.wantChange)
			}
		})
	}
}

func TestWeakPasswordPolicy(t *testing.T) {
	cases := []struct {
		password string
		username string
		want     bool
	}{
		{password: "short", want: true},
		{password: "admin123", want: true},
		{password: "operator", username: "operator", want: true},
		{password: "123456789012", want: true},
		{password: "abcdefgh", want: true},
		{password: "correcthorsebattery", want: false},
		{password: "StrongPass9!", want: false},
	}
	for _, tc := range cases {
		if got := isWeakPassword(tc.password, tc.username); got != tc.want {
			t.Fatalf("isWeakPassword(%q, %q)=%v want %v", tc.password, tc.username, got, tc.want)
		}
	}
}

func TestChangePasswordPersistsHashAndIssuesReplacementToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	initial := "web:\n  username: admin\n  password: bootstrap-password\n"
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{
		auth:       config.WebConfig{Username: "admin", Password: "bootstrap-password", PasswordSource: config.WebPasswordSourceConfigFile},
		configPath: configPath,
	}
	oldToken, _, err := server.issueSessionToken()
	if err != nil {
		t.Fatal(err)
	}

	recorder := changePasswordRequest(t, server, `{"old_password":"bootstrap-password","new_password":"NewPassword9!","confirm_password":"NewPassword9!"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Token      string                   `json:"token"`
		Credential passwordCredentialStatus `json:"credential"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Token == "" || !server.isSessionTokenValid(response.Token, time.Now()) {
		t.Fatal("replacement token is missing or invalid")
	}
	if server.isSessionTokenValid(oldToken, time.Now()) {
		t.Fatal("old token remained valid after password change")
	}
	if response.Credential.ChangeRequired {
		t.Fatal("strong replacement password still requires change")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "NewPassword9!") {
		t.Fatal("configuration contains the plaintext replacement password")
	}
	if !checkPassword(server.authSnapshot().Password, "NewPassword9!") {
		t.Fatal("persisted bcrypt password does not match replacement password")
	}
}

func TestChangePasswordRejectsEnvironmentManagedAndWeakPasswords(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	initial := "web:\n  username: admin\n  password: file-password\n"
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	environmentServer := &Server{
		auth:       config.WebConfig{Username: "admin", Password: "environment-password", PasswordSource: config.WebPasswordSourceEnvironment},
		configPath: configPath,
	}
	response := changePasswordRequest(t, environmentServer, `{"old_password":"environment-password","new_password":"NewPassword9!","confirm_password":"NewPassword9!"}`)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "password_managed_by_environment") {
		t.Fatalf("environment response status=%d body=%s", response.Code, response.Body.String())
	}

	configServer := &Server{
		auth:       config.WebConfig{Username: "admin", Password: "file-password", PasswordSource: config.WebPasswordSourceConfigFile},
		configPath: configPath,
	}
	response = changePasswordRequest(t, configServer, `{"old_password":"file-password","new_password":"password","confirm_password":"password"}`)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "weak_password") {
		t.Fatalf("weak response status=%d body=%s", response.Code, response.Body.String())
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != initial {
		t.Fatalf("rejected password modified config:\n%s", data)
	}
}

func loginForCredentialStatus(t *testing.T, server *Server, password string) struct {
	Credential passwordCredentialStatus `json:"credential"`
} {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/auth/login", server.handleLogin)
	recorder := httptest.NewRecorder()
	body := `{"username":"admin","password":"` + password + `"}`
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Credential passwordCredentialStatus `json:"credential"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func changePasswordRequest(t *testing.T, server *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/settings/password", server.handleChangePassword)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/settings/password", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}
