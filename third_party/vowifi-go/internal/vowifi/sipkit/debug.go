package sipkit

import "strings"

const (
	maxFirstLineLength = 200
	maxDebugBodyLength = 240
)

// FirstLine returns a compact first line from a raw SIP message.
func FirstLine(raw string) string {
	line := strings.TrimSpace(raw)
	if end := strings.Index(line, "\r\n"); end >= 0 {
		line = strings.TrimSpace(line[:end])
	}
	return DebugText(line, maxFirstLineLength)
}

// SanitizeBody flattens a body for bounded debug logging.
func SanitizeBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	text := strings.TrimSpace(string(body))
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	return DebugText(text, maxDebugBodyLength)
}

// DebugText bounds a string for protocol debug output.
func DebugText(text string, limit int) string {
	if limit >= 0 && len(text) > limit {
		return text[:limit]
	}
	return text
}
