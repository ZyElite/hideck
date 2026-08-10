package voice

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	sdpAudioPrefix      = "m=audio"
	sdpCryptoPrefix     = "a=crypto:"
	sdpRTPMapPrefix     = "a=rtpmap:"
	sdpFMTPPrefix       = "a=fmtp:"
	sdpRTCPPrefix       = "a=rtcp:"
	sdpTelephoneEvent   = "telephone-event"
	sdpSecureRTPProfile = "RTP/SAVP"
	sdpPlainRTPProfile  = "RTP/AVP"
	dynamicPayloadStart = 96
	telephoneEventFMTP  = "0-16"
	telephoneEventClock = 8000
)

var sdpAddressPattern = regexp.MustCompile(`(IN\s+)IP[46]\s+[^\s]+`)

// RewriteSDP rewrites an SDP media endpoint using the recovered byte API.
func RewriteSDP(raw []byte, localIP string, localPort int) []byte {
	if len(raw) == 0 {
		return nil
	}
	lines := splitSDPLines(raw)
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	rewritten := make([]string, 0, len(lines))
	for _, source := range lines {
		line := strings.TrimSuffix(source, "\r")
		if strings.HasPrefix(line, sdpCryptoPrefix) {
			continue
		}
		rewritten = append(rewritten, rewriteBasicSDPLine(line, localIP, localPort))
	}
	return []byte(strings.Join(rewritten, "\r\n"))
}

func rewriteBasicSDPLine(line, localIP string, localPort int) string {
	switch {
	case strings.HasPrefix(line, "o="):
		return rewriteSDPOrigin(line, localIP)
	case strings.HasPrefix(line, "c="):
		return sdpConnectionLine(localIP)
	case strings.HasPrefix(line, sdpAudioPrefix):
		return rewriteAudioLine(line, localPort, "", nil)
	case strings.HasPrefix(line, sdpRTCPPrefix):
		return sdpRTCPPrefix + strconv.Itoa(localPort+1)
	default:
		return line
	}
}

// RewriteSDPForClient restores codec/PT translation against a client offer.
func RewriteSDPForClient(
	raw []byte,
	localIP string,
	localPort int,
	clientSDP []byte,
) ([]byte, map[int]int) {
	clientProtocol, clientCrypto := clientSDPMetadata(clientSDP)
	clientInfo, _ := ParseSDP(clientSDP)
	remoteInfo, _ := ParseSDP(raw)
	mapping := codecPTMapping(remoteInfo, clientInfo)
	lines := splitSDPLines(raw)
	for index, source := range lines {
		line := strings.TrimSpace(source)
		lines[index] = rewriteClientSDPLine(line, localIP, localPort, clientProtocol, mapping)
	}
	lines = ensureTelephoneEvent(lines, clientInfo)
	lines = insertSDPLines(lines, clientCrypto)
	return []byte(strings.Join(lines, "\r\n")), mapping
}

func splitSDPLines(raw []byte) []string {
	lines := strings.Split(string(raw), "\r\n")
	if len(lines) == 1 {
		lines = strings.Split(string(raw), "\n")
	}
	return lines
}

func clientSDPMetadata(raw []byte) (string, []string) {
	var protocol string
	var crypto []string
	for _, source := range splitSDPLines(raw) {
		line := strings.TrimSpace(source)
		if strings.HasPrefix(line, sdpAudioPrefix) {
			fields := strings.Fields(line)
			if len(fields) > 2 {
				protocol = fields[2]
			}
		}
		if strings.HasPrefix(line, sdpCryptoPrefix) {
			crypto = append(crypto, line)
		}
	}
	return protocol, crypto
}

func codecPTMapping(remote, client *SDPInfo) map[int]int {
	mapping := make(map[int]int)
	if remote == nil || client == nil {
		return mapping
	}
	for _, remoteCodec := range remote.Codecs {
		if remoteCodec.PayloadType < dynamicPayloadStart {
			continue
		}
		clientCodec := client.FindCodec(remoteCodec.Name, remoteCodec.ClockRate, remoteCodec.Channels)
		if clientCodec != nil && clientCodec.PayloadType != remoteCodec.PayloadType {
			mapping[remoteCodec.PayloadType] = clientCodec.PayloadType
		}
	}
	return mapping
}

func rewriteClientSDPLine(
	line, localIP string,
	localPort int,
	clientProtocol string,
	mapping map[int]int,
) string {
	switch {
	case strings.HasPrefix(line, "o="):
		return rewriteSDPOrigin(line, localIP)
	case strings.HasPrefix(line, "c="):
		return sdpConnectionLine(localIP)
	case strings.HasPrefix(line, sdpAudioPrefix):
		return rewriteAudioLine(line, localPort, clientProtocol, mapping)
	case strings.HasPrefix(line, sdpRTPMapPrefix):
		return rewriteAttributePayload(line, sdpRTPMapPrefix, mapping)
	case strings.HasPrefix(line, sdpFMTPPrefix):
		return rewriteAttributePayload(line, sdpFMTPPrefix, mapping)
	case strings.HasPrefix(line, sdpRTCPPrefix):
		return sdpRTCPPrefix + strconv.Itoa(localPort+1)
	default:
		return line
	}
}

func rewriteSDPOrigin(line, localIP string) string {
	replacement := "${1}" + sdpIPFamily(localIP) + " " + localIP
	return sdpAddressPattern.ReplaceAllString(line, replacement)
}

func sdpConnectionLine(localIP string) string {
	return "c=IN " + sdpIPFamily(localIP) + " " + localIP
}

func rewriteAudioLine(line string, port int, protocol string, mapping map[int]int) string {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return strings.Replace(line, sdpSecureRTPProfile, sdpPlainRTPProfile, 1)
	}
	fields[1] = strconv.Itoa(port)
	if protocol != "" {
		fields[2] = protocol
	} else if fields[2] == sdpSecureRTPProfile {
		fields[2] = sdpPlainRTPProfile
	}
	for index := 3; index < len(fields); index++ {
		payloadType, err := strconv.Atoi(fields[index])
		mappedPayloadType, exists := mapping[payloadType]
		if err == nil && exists {
			fields[index] = strconv.Itoa(mappedPayloadType)
		}
	}
	return strings.Join(fields, " ")
}

func rewriteAttributePayload(line, prefix string, mapping map[int]int) string {
	rest := strings.TrimPrefix(line, prefix)
	parts := strings.SplitN(rest, " ", 2)
	payloadType, err := strconv.Atoi(parts[0])
	mappedPayloadType, exists := mapping[payloadType]
	if err != nil || !exists {
		return line
	}
	parts[0] = strconv.Itoa(mappedPayloadType)
	return prefix + strings.Join(parts, " ")
}

func ensureTelephoneEvent(lines []string, client *SDPInfo) []string {
	if payloadType := telephoneEventPayload(lines); payloadType > 0 {
		fmtp := fmt.Sprintf("%s%d", sdpFMTPPrefix, payloadType)
		if !containsSDPLinePrefix(lines, fmtp) {
			lines = insertSDPLines(lines, []string{fmt.Sprintf("%s%d %s", sdpFMTPPrefix, payloadType, telephoneEventFMTP)})
		}
		return lines
	}
	if client == nil {
		return lines
	}
	codec := client.FindCodec(sdpTelephoneEvent, 0, 0)
	if codec == nil || codec.PayloadType <= 0 {
		return lines
	}
	for index, line := range lines {
		if strings.HasPrefix(line, sdpAudioPrefix) {
			lines[index] = line + " " + strconv.Itoa(codec.PayloadType)
			break
		}
	}
	attributes := []string{
		fmt.Sprintf("%s%d %s/%d", sdpRTPMapPrefix, codec.PayloadType, sdpTelephoneEvent, telephoneEventClock),
		fmt.Sprintf("%s%d %s", sdpFMTPPrefix, codec.PayloadType, telephoneEventFMTP),
	}
	return insertSDPLines(lines, attributes)
}

func telephoneEventPayload(lines []string) int {
	for _, line := range lines {
		if !strings.HasPrefix(line, sdpRTPMapPrefix) ||
			!strings.Contains(strings.ToLower(line), sdpTelephoneEvent) {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, sdpRTPMapPrefix))
		if len(fields) > 0 {
			payloadType, _ := strconv.Atoi(fields[0])
			return payloadType
		}
	}
	return 0
}

func containsSDPLinePrefix(lines []string, target string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, target) {
			return true
		}
	}
	return false
}

func insertSDPLines(lines, additions []string) []string {
	if len(additions) == 0 {
		return lines
	}
	index := len(lines)
	for index > 0 && strings.TrimSpace(lines[index-1]) == "" {
		index--
	}
	result := make([]string, 0, len(lines)+len(additions))
	result = append(result, lines[:index]...)
	result = append(result, additions...)
	return append(result, lines[index:]...)
}
