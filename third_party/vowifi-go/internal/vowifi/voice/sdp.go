package voice

import (
	"errors"
	"strconv"
	"strings"
)

// ParseSDP parses an SDP session description (RFC 4566).
func ParseSDP(sdp string) (*SDPInfo, error) {
	if strings.TrimSpace(sdp) == "" {
		return nil, errors.New("voice: empty SDP")
	}
	info := &SDPInfo{}
	var cur *MediaInfo
	for _, line := range strings.Split(sdp, "\r\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) < 2 || line[1] != '=' {
			continue
		}
		key := line[0]
		value := line[2:]
		switch key {
		case 'o':
			info.Origin = value
		case 's':
			info.SessionName = value
		case 'c':
			if cur != nil {
				cur.Connection = value
			} else {
				info.Connection = value
			}
		case 'm':
			m, err := parseMediaLine(value)
			if err != nil {
				return nil, err
			}
			info.Media = append(info.Media, *m)
			cur = &info.Media[len(info.Media)-1]
		case 'a':
			if cur != nil {
				applyAttribute(cur, value)
			}
		}
	}
	if len(info.Media) == 0 {
		return nil, errors.New("voice: SDP has no media lines")
	}
	return info, nil
}

// parseMediaLine parses an m= line.
func parseMediaLine(value string) (*MediaInfo, error) {
	parts := strings.Fields(value)
	if len(parts) < 3 {
		return nil, errors.New("voice: malformed m= line")
	}
	m := &MediaInfo{
		Type:     parts[0],
		Protocol: parts[2],
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, errors.New("voice: malformed m= port")
	}
	m.Port = port
	for _, f := range parts[3:] {
		pt, err := strconv.Atoi(f)
		if err != nil {
			continue
		}
		m.Formats = append(m.Formats, pt)
	}
	return m, nil
}

// applyAttribute applies an a= attribute to a media section.
func applyAttribute(m *MediaInfo, value string) {
	switch {
	case strings.HasPrefix(value, "rtpmap:"):
		codec, err := parseRTPMap(value[len("rtpmap:"):])
		if err == nil {
			m.Codecs = append(m.Codecs, *codec)
		}
	case strings.HasPrefix(value, "fmtp:"):
		rest := value[len("fmtp:"):]
		ptStr, fmtp, _ := strings.Cut(rest, " ")
		pt, err := strconv.Atoi(ptStr)
		if err != nil {
			return
		}
		for i := range m.Codecs {
			if m.Codecs[i].PayloadType == pt {
				m.Codecs[i].Fmtp = fmtp
				return
			}
		}
	}
}

// parseRTPMap parses an rtpmap attribute.
func parseRTPMap(value string) (*CodecInfo, error) {
	ptStr, rest, ok := strings.Cut(value, " ")
	if !ok {
		return nil, errors.New("voice: malformed rtpmap")
	}
	pt, err := strconv.Atoi(ptStr)
	if err != nil {
		return nil, err
	}
	codec := &CodecInfo{PayloadType: pt}
	enc := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		enc = rest[:i]
		rateStr := rest[i+1:]
		if j := strings.IndexByte(rateStr, '/'); j >= 0 {
			chStr := rateStr[j+1:]
			rateStr = rateStr[:j]
			if ch, err := strconv.Atoi(chStr); err == nil {
				codec.Channels = ch
			}
		}
		if rate, err := strconv.Atoi(rateStr); err == nil {
			codec.ClockRate = rate
		}
	}
	codec.Encoding = enc
	return codec, nil
}

// FindCodec returns the codec with the given payload type.
func (s *SDPInfo) FindCodec(pt int) *CodecInfo {
	if s == nil {
		return nil
	}
	for i := range s.Media {
		for j := range s.Media[i].Codecs {
			if s.Media[i].Codecs[j].PayloadType == pt {
				return &s.Media[i].Codecs[j]
			}
		}
	}
	return nil
}

// GetMediaAddress returns the media connection address (the IP only).
func (s *SDPInfo) GetMediaAddress() string {
	if s == nil {
		return ""
	}
	conn := s.Connection
	if len(s.Media) > 0 && s.Media[0].Connection != "" {
		conn = s.Media[0].Connection
	}
	// c=IN IP4 10.0.0.1 -> 10.0.0.1
	fields := strings.Fields(conn)
	if len(fields) >= 3 {
		return fields[2]
	}
	return conn
}

// GetMediaPort returns the first media port.
func (s *SDPInfo) GetMediaPort() int {
	if s == nil || len(s.Media) == 0 {
		return 0
	}
	return s.Media[0].Port
}

// RewriteSDP rewrites the connection address and port in an SDP body.
func RewriteSDP(sdp, ip string, port int) string {
	if sdp == "" {
		return sdp
	}
	var b strings.Builder
	for _, line := range strings.Split(sdp, "\r\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "c="):
			b.WriteString("c=IN IP4 " + ip + "\r\n")
			continue
		case strings.HasPrefix(line, "m="):
			parts := strings.Fields(line)
			if len(parts) >= 3 && port > 0 {
				b.WriteString(parts[0] + " " + strconv.Itoa(port) + " " + strings.Join(parts[2:], " ") + "\r\n")
				continue
			}
		}
		b.WriteString(line + "\r\n")
	}
	return b.String()
}

// ExtractAndApplyPTMapping builds a LAN->IMS payload type mapping from an
// SDP answer.
func ExtractAndApplyPTMapping(offer, answer *SDPInfo) map[int]int {
	mapping := make(map[int]int)
	if offer == nil || answer == nil {
		return mapping
	}
	for _, om := range offer.Media {
		for _, oc := range om.Codecs {
			for _, am := range answer.Media {
				for _, ac := range am.Codecs {
					if oc.Encoding == ac.Encoding && oc.ClockRate == ac.ClockRate {
						mapping[ac.PayloadType] = oc.PayloadType
					}
				}
			}
		}
	}
	return mapping
}
