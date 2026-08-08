package e911

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

func parseEAPRelayPacket(value interface{}, result *entitlementResult) {
	raw, ok := decodeChallengeBytes(stringValue(value))
	if !ok || len(raw) == 0 {
		return
	}
	packet, err := eapaka.ParsePacket(raw)
	if err != nil {
		return
	}
	result.EAPPacket = &packet
	result.EAPPacketRaw = append([]byte(nil), raw...)
	rand16, autn16, err := eapaka.ChallengeRANDAndAUTN(packet)
	if err != nil {
		return
	}
	if len(result.RAND) == 0 {
		result.RAND = rand16
	}
	if len(result.AUTN) == 0 {
		result.AUTN = autn16
	}
}

func parseCombinedChallenge(value interface{}, result *entitlementResult) {
	raw, ok := decodeChallengeBytes(stringValue(value))
	if !ok || len(raw) < 32 {
		return
	}
	if len(result.RAND) == 0 {
		result.RAND = append([]byte(nil), raw[:16]...)
	}
	if len(result.AUTN) == 0 {
		result.AUTN = append([]byte(nil), raw[16:32]...)
	}
}

func decodeChallengeBytes(value string) ([]byte, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}
	clean := strings.NewReplacer(" ", "", ":", "", "-", "").Replace(value)
	if raw, err := hex.DecodeString(clean); err == nil {
		return raw, true
	}
	if raw, err := base64.StdEncoding.DecodeString(value); err == nil {
		return raw, true
	}
	if raw, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return raw, true
	}
	return nil, false
}

func numberValue(value interface{}) (int, bool) {
	switch current := value.(type) {
	case float64:
		return int(current), true
	case int:
		return current, true
	case json.Number:
		number, err := current.Int64()
		return int(number), err == nil
	case string:
		var number int
		_, err := fmt.Sscanf(strings.TrimSpace(current), "%d", &number)
		return number, err == nil
	default:
		return 0, false
	}
}

func stringValue(value interface{}) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func looksHTTPURL(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
