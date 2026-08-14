package api

import (
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/config"
)

const defaultWebPassword = "admin"

const (
	minimumPasswordLength       = 8
	singleClassPassphraseLength = 12
)

type passwordCredentialStatus struct {
	ChangeRequired      bool   `json:"change_required"`
	Management          string `json:"management"`
	EnvironmentVariable string `json:"environment_variable,omitempty"`
}

type authenticatedSession struct {
	Token      string
	ExpiresAt  time.Time
	Credential passwordCredentialStatus
}

func isBcryptPassword(password string) bool {
	return strings.HasPrefix(password, "$2a$") ||
		strings.HasPrefix(password, "$2b$") ||
		strings.HasPrefix(password, "$2y$")
}

func passwordStatusFor(credentials config.WebConfig, suppliedPassword string) passwordCredentialStatus {
	if credentials.PasswordSource == config.WebPasswordSourceEnvironment {
		return passwordCredentialStatus{
			ChangeRequired:      passwordRequiresChange(credentials, suppliedPassword),
			Management:          string(config.WebPasswordSourceEnvironment),
			EnvironmentVariable: config.WebPasswordEnvironmentVariable,
		}
	}
	return passwordCredentialStatus{
		ChangeRequired: passwordRequiresChange(credentials, suppliedPassword),
		Management:     string(config.WebPasswordSourceConfigFile),
	}
}

func passwordRequiresChange(credentials config.WebConfig, suppliedPassword string) bool {
	if !isBcryptPassword(credentials.Password) {
		if credentials.PasswordSource != config.WebPasswordSourceEnvironment {
			return true
		}
		return isWeakPassword(credentials.Password, credentials.Username)
	}
	return suppliedPassword != "" && isWeakPassword(suppliedPassword, credentials.Username)
}

func isWeakPassword(password string, username string) bool {
	normalized := strings.TrimSpace(password)
	runes := []rune(normalized)
	if len(runes) < minimumPasswordLength {
		return true
	}
	if username != "" && strings.EqualFold(normalized, strings.TrimSpace(username)) {
		return true
	}
	lowercase := strings.ToLower(normalized)
	if isCommonPassword(lowercase) || containsObviousSequence(lowercase) || containsSingleRepeatedRune(runes) {
		return true
	}
	return len(runes) < singleClassPassphraseLength && passwordCharacterClasses(runes) < 2
}

func containsObviousSequence(password string) bool {
	sequences := []string{
		"0123456789", "1234567890", "9876543210",
		"abcdefghijklmnopqrstuvwxyz", "zyxwvutsrqponmlkjihgfedcba",
		"qwertyuiop", "poiuytrewq", "asdfghjkl", "lkjhgfdsa",
	}
	for _, sequence := range sequences {
		if strings.Contains(password, sequence) || strings.Contains(sequence, password) {
			return true
		}
	}
	return false
}

func isCommonPassword(password string) bool {
	switch password {
	case defaultWebPassword, "password", "password1", "12345678", "123456789", "qwerty123", "admin123", "hideck123":
		return true
	default:
		return false
	}
}

func containsSingleRepeatedRune(runes []rune) bool {
	for i := 1; i < len(runes); i++ {
		if runes[i] != runes[0] {
			return false
		}
	}
	return len(runes) > 0
}

func passwordCharacterClasses(runes []rune) int {
	classes := 0
	hasLower, hasUpper, hasDigit, hasSymbol := false, false, false, false
	for _, r := range runes {
		hasLower = hasLower || unicode.IsLower(r)
		hasUpper = hasUpper || unicode.IsUpper(r)
		hasDigit = hasDigit || unicode.IsDigit(r)
		hasSymbol = hasSymbol || (!unicode.IsLetter(r) && !unicode.IsDigit(r))
	}
	for _, present := range []bool{hasLower, hasUpper, hasDigit, hasSymbol} {
		if present {
			classes++
		}
	}
	return classes
}

func (s *Server) authSnapshot() config.WebConfig {
	s.authMu.RLock()
	defer s.authMu.RUnlock()
	return s.auth
}

func (s *Server) createLoginSession(username string, password string) (authenticatedSession, bool, error) {
	s.authChangeMu.Lock()
	defer s.authChangeMu.Unlock()
	credentials := s.authSnapshot()
	if username != credentials.Username || !checkPassword(credentials.Password, password) {
		return authenticatedSession{}, false, nil
	}
	token, expiresAt, err := s.issueSessionToken()
	if err != nil {
		return authenticatedSession{}, true, err
	}
	return authenticatedSession{
		Token:      token,
		ExpiresAt:  expiresAt,
		Credential: s.passwordStatus(password),
	}, true, nil
}

func (s *Server) setAuthPassword(password string) {
	s.authMu.Lock()
	s.auth.Password = password
	s.auth.PasswordSource = config.WebPasswordSourceConfigFile
	s.passwordChangeRequired = false
	s.authMu.Unlock()
}

func (s *Server) passwordStatus(suppliedPassword string) passwordCredentialStatus {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	status := passwordStatusFor(s.auth, suppliedPassword)
	if status.ChangeRequired {
		s.passwordChangeRequired = true
	}
	status.ChangeRequired = s.passwordChangeRequired
	return status
}

func (s *Server) handleGetPasswordStatus(c *gin.Context) {
	c.JSON(http.StatusOK, s.passwordStatus(""))
}
