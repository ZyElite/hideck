package voice

import (
	"errors"
	"strconv"
	"strings"
)

// ParseSDPCurrent retains the displaced multi-section string parser.
func ParseSDPCurrent(raw string) (*SDPInfoCurrent, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("voice: empty SDP")
	}
	info := &SDPInfoCurrent{}
	var current *MediaInfo
	for _, source := range strings.Split(raw, "\r\n") {
		line := strings.TrimSpace(source)
		if len(line) < 2 || line[1] != '=' {
			continue
		}
		var err error
		current, err = applyCurrentSDPLine(info, current, line[0], line[2:])
		if err != nil {
			return nil, err
		}
	}
	if len(info.Media) == 0 {
		return nil, errors.New("voice: SDP has no media lines")
	}
	return info, nil
}

func applyCurrentSDPLine(
	info *SDPInfoCurrent,
	current *MediaInfo,
	key byte,
	value string,
) (*MediaInfo, error) {
	switch key {
	case 'o':
		info.Origin = value
	case 's':
		info.SessionName = value
	case 'c':
		applyCurrentConnection(info, current, value)
	case 'm':
		mediaInfo, err := parseCurrentMediaLine(value)
		if err != nil {
			return current, err
		}
		info.Media = append(info.Media, *mediaInfo)
		current = &info.Media[len(info.Media)-1]
	case 'a':
		applyCurrentAttribute(current, value)
	}
	return current, nil
}

func applyCurrentConnection(info *SDPInfoCurrent, current *MediaInfo, value string) {
	if current != nil {
		current.Connection = value
		return
	}
	info.Connection = value
}

func parseCurrentMediaLine(value string) (*MediaInfo, error) {
	parts := strings.Fields(value)
	if len(parts) < 3 {
		return nil, errors.New("voice: malformed m= line")
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, errors.New("voice: malformed m= port")
	}
	info := &MediaInfo{Type: parts[0], Port: port, Protocol: parts[2]}
	for _, field := range parts[3:] {
		if payloadType, parseErr := strconv.Atoi(field); parseErr == nil {
			info.Formats = append(info.Formats, payloadType)
		}
	}
	return info, nil
}

func applyCurrentAttribute(info *MediaInfo, value string) {
	if info == nil {
		return
	}
	switch {
	case strings.HasPrefix(value, "rtpmap:"):
		if codec, err := parseCurrentRTPMap(strings.TrimPrefix(value, "rtpmap:")); err == nil {
			info.Codecs = append(info.Codecs, *codec)
		}
	case strings.HasPrefix(value, "fmtp:"):
		applyCurrentFMTP(info, strings.TrimPrefix(value, "fmtp:"))
	}
}

func parseCurrentRTPMap(value string) (*CodecInfoCurrent, error) {
	payload, encoding, ok := strings.Cut(value, " ")
	if !ok {
		return nil, errors.New("voice: malformed rtpmap")
	}
	payloadType, err := strconv.Atoi(payload)
	if err != nil {
		return nil, err
	}
	codec := &CodecInfoCurrent{PayloadType: payloadType}
	parts := strings.Split(encoding, "/")
	codec.Encoding = parts[0]
	if len(parts) > 1 {
		codec.ClockRate, _ = strconv.Atoi(parts[1])
	}
	if len(parts) > 2 {
		codec.Channels, _ = strconv.Atoi(parts[2])
	}
	return codec, nil
}

func applyCurrentFMTP(info *MediaInfo, value string) {
	payload, format, _ := strings.Cut(value, " ")
	payloadType, err := strconv.Atoi(payload)
	if err != nil {
		return
	}
	for index := range info.Codecs {
		if info.Codecs[index].PayloadType == payloadType {
			info.Codecs[index].Fmtp = format
			return
		}
	}
}

// FindCodec returns the codec with payloadType from the displaced model.
func (s *SDPInfoCurrent) FindCodec(payloadType int) *CodecInfoCurrent {
	if s == nil {
		return nil
	}
	for mediaIndex := range s.Media {
		for codecIndex := range s.Media[mediaIndex].Codecs {
			codec := &s.Media[mediaIndex].Codecs[codecIndex]
			if codec.PayloadType == payloadType {
				return codec
			}
		}
	}
	return nil
}

// GetMediaAddress returns the first effective media IP from the displaced model.
func (s *SDPInfoCurrent) GetMediaAddress() string {
	if s == nil {
		return ""
	}
	connection := s.Connection
	if len(s.Media) > 0 && s.Media[0].Connection != "" {
		connection = s.Media[0].Connection
	}
	fields := strings.Fields(connection)
	if len(fields) >= 3 {
		return fields[2]
	}
	return connection
}

// GetMediaPort returns the first media port from the displaced model.
func (s *SDPInfoCurrent) GetMediaPort() int {
	if s == nil || len(s.Media) == 0 {
		return 0
	}
	return s.Media[0].Port
}

// RewriteSDPCurrent retains the displaced string rewriter behavior.
func RewriteSDPCurrent(raw, localIP string, localPort int) string {
	if raw == "" {
		return raw
	}
	var rewritten strings.Builder
	addressFamily := sdpIPFamily(localIP)
	for _, source := range strings.Split(raw, "\r\n") {
		line := strings.TrimSpace(source)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "c="):
			line = "c=IN " + addressFamily + " " + localIP
		case strings.HasPrefix(line, "m=") && localPort > 0:
			line = rewriteCurrentMediaPort(line, localPort)
		}
		rewritten.WriteString(line)
		rewritten.WriteString("\r\n")
	}
	return rewritten.String()
}

func rewriteCurrentMediaPort(line string, localPort int) string {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return line
	}
	return fields[0] + " " + strconv.Itoa(localPort) + " " + strings.Join(fields[2:], " ")
}

// RewriteSDPForClientCurrent retains the displaced three-argument helper.
func RewriteSDPForClientCurrent(raw, localIP string, localPort int) string {
	return RewriteSDPCurrent(raw, localIP, localPort)
}
