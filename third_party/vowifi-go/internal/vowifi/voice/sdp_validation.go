package voice

import (
	"fmt"
	"net"
	"strings"
)

func validateSDPMediaEndpoint(raw []byte, source string) error {
	info, err := ParseSDP(raw)
	if err != nil {
		return fmt.Errorf("voice: parse %s SDP: %w", source, err)
	}
	if info == nil {
		return fmt.Errorf("voice: %s has an invalid SDP media endpoint", source)
	}
	ip := strings.Trim(strings.TrimSpace(info.ConnectionIP), "[]")
	if net.ParseIP(ip) == nil || info.MediaPort <= 0 {
		return fmt.Errorf("voice: %s has an invalid SDP media endpoint", source)
	}
	return nil
}
