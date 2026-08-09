package imscore

import (
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

func buildSIPRequestResponse(request string, status int) (string, error) {
	return buildSIPVoiceResponse(request, newTag(), InboundVoiceResponse{StatusCode: status})
}
