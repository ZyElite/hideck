package imscore

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const maxSIPDebugRawLength = 8 * 1024

func sipDebugRawText(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) > maxSIPDebugRawLength {
		raw = raw[:maxSIPDebugRawLength] + "...[truncated]"
	}
	if diagnosticEnvironmentEnabled("VOHIVE_SIP_RAW_LOG") {
		return raw
	}
	return logging.RedactSIPRaw(raw)
}

func diagnosticEnvironmentEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isSoftMOSubmitProbeTimeout(err error, sipCode int, transport string) bool {
	if err == nil || sipCode > 0 || !strings.EqualFold(strings.TrimSpace(transport), "UDP") {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "i/o timeout") || strings.Contains(message, "deadline exceeded")
}

func parseRPErrorCause(value string) (int, bool) {
	const marker = "rp-error cause "
	lower := strings.ToLower(value)
	index := strings.Index(lower, marker)
	if index < 0 {
		return 0, false
	}
	start := index + len(marker)
	end := start
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	if end == start {
		return 0, false
	}
	cause, err := strconv.Atoi(value[start:end])
	return cause, err == nil
}

func appendRPErrorFields(fields []interface{}, value string) []interface{} {
	cause, ok := parseRPErrorCause(value)
	if !ok {
		return fields
	}
	return append(fields, "rp_cause", cause, "rp_cause_text", rpCauseText(cause))
}

func rpCauseText(cause int) string {
	causes := map[int]string{
		21: "short message transfer rejected", 22: "memory capacity exceeded",
		28: "unidentified subscriber", 29: "facility rejected", 30: "unknown subscriber",
		38: "network out of order", 41: "temporary failure", 42: "congestion",
		47: "resources unavailable", 50: "requested facility not subscribed",
		69: "requested facility not implemented", 95: "semantically incorrect message",
		96: "invalid mandatory information", 97: "message type not implemented",
		98: "message incompatible with protocol state", 111: "protocol error",
	}
	if text := causes[cause]; text != "" {
		return text
	}
	return "unknown"
}

func normalizeE164(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "+") || len(value) <= 6 {
		return value
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return value
		}
	}
	return "+" + value
}
