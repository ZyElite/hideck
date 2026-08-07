package imscore

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

func (s *Service) respondToInboundNotification(conn net.Conn, raw string) error {
	if !strings.EqualFold(sipRequestMethod(raw), "NOTIFY") {
		return nil
	}
	response, err := buildSIPRequestResponse(raw, 200)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(conn, response); err != nil {
		return fmt.Errorf("write NOTIFY response: %w", err)
	}
	logging.Info("IMS NOTIFY(reg) 已确认", "event", rawSIPHeaderValue(raw, "Event"))
	return nil
}

func sipRequestMethod(raw string) string {
	line, _, _ := strings.Cut(raw, "\n")
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) != 3 || !strings.EqualFold(fields[2], "SIP/2.0") {
		return ""
	}
	return fields[0]
}

func buildSIPRequestResponse(request string, status int) (string, error) {
	if sipRequestMethod(request) == "" {
		return "", errors.New("imscore: invalid inbound SIP request line")
	}
	required := []string{"Via", "From", "To", "Call-ID", "CSeq"}
	headers := make(map[string]string, len(required))
	for _, name := range required {
		headers[name] = rawSIPHeaderValue(request, name)
		if headers[name] == "" {
			return "", fmt.Errorf("imscore: inbound SIP request missing %s", name)
		}
	}
	to := headers["To"]
	if !strings.Contains(strings.ToLower(to), ";tag=") {
		to += ";tag=" + newTag()
	}
	return fmt.Sprintf(
		"SIP/2.0 %d %s\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
		status, SIPStatusText(status), headers["Via"], headers["From"], to, headers["Call-ID"], headers["CSeq"],
	), nil
}

func rawSIPHeaderValue(message, name string) string {
	for _, line := range strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n") {
		headerName, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(headerName), name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
