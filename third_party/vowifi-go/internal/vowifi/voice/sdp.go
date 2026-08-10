package voice

import (
	"bytes"
	"net"
	"strconv"
	"strings"
)

const defaultCodecChannels = 1

// ParseSDP recovers the v1.5.5 byte-oriented audio SDP parser.
func ParseSDP(raw []byte) (*SDPInfo, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	info := &SDPInfo{RawSDP: raw}
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.TrimRight(line, "\r")
		switch {
		case bytes.HasPrefix(line, []byte("c=")):
			parseConnectionLine(info, line[2:])
		case bytes.HasPrefix(line, []byte("m=")):
			parseAudioMediaLine(info, line[2:])
		case bytes.HasPrefix(line, []byte("a=rtpmap:")):
			if codec := parseRTPMapBytes(line[len("a=rtpmap:"):]); codec != nil {
				info.Codecs = append(info.Codecs, *codec)
			}
		case bytes.HasPrefix(line, []byte("a=fmtp:")):
			applyCodecFMTP(info, line[len("a=fmtp:"):])
		}
	}
	return info, nil
}

func parseConnectionLine(info *SDPInfo, value []byte) {
	fields := bytes.Fields(value)
	if len(fields) >= 3 {
		info.ConnectionIP = string(fields[2])
	}
}

func parseAudioMediaLine(info *SDPInfo, value []byte) {
	fields := bytes.Fields(value)
	if len(fields) == 0 {
		return
	}
	info.MediaType = string(fields[0])
	if len(fields) < 2 {
		return
	}
	if port, err := strconv.Atoi(string(fields[1])); err == nil {
		info.MediaPort = port
	}
}

func applyCodecFMTP(info *SDPInfo, value []byte) {
	fields := bytes.Fields(value)
	if len(fields) < 2 {
		return
	}
	payloadType, err := strconv.Atoi(string(fields[0]))
	if err != nil {
		return
	}
	fmtp := codecAttributeRemainder(value)
	if codec := info.GetCodecByPT(payloadType); codec != nil {
		codec.Fmtp = fmtp
	}
}

func codecAttributeRemainder(value []byte) string {
	value = bytes.TrimSpace(value)
	for index, char := range value {
		if char == ' ' || char == '\t' {
			return strings.TrimSpace(string(value[index:]))
		}
	}
	return ""
}

func parseRTPMapBytes(value []byte) *CodecInfo {
	fields := bytes.Fields(value)
	if len(fields) < 2 {
		return nil
	}
	payloadType, err := strconv.Atoi(string(fields[0]))
	if err != nil {
		return nil
	}
	parts := bytes.Split(fields[1], []byte{'/'})
	if len(parts) < 2 {
		return nil
	}
	codec := &CodecInfo{
		PayloadType: payloadType,
		Name:        string(parts[0]),
		Channels:    defaultCodecChannels,
	}
	if clockRate, parseErr := strconv.Atoi(string(parts[1])); parseErr == nil {
		codec.ClockRate = clockRate
	}
	if len(parts) > 2 {
		if channels, parseErr := strconv.Atoi(string(parts[2])); parseErr == nil {
			codec.Channels = channels
		}
	}
	return codec
}

// FindCodec returns a codec matching name, clock rate and channel count.
// Non-positive rate/channel arguments retain the recovered wildcard behavior.
func (s *SDPInfo) FindCodec(name string, clockRate, channels int) *CodecInfo {
	if s == nil {
		return nil
	}
	for index := range s.Codecs {
		codec := &s.Codecs[index]
		if !strings.EqualFold(codec.Name, name) {
			continue
		}
		if clockRate > 0 && codec.ClockRate != clockRate {
			continue
		}
		if channels > 0 && codec.Channels != channels {
			continue
		}
		return codec
	}
	return nil
}

// GetCodecByPT returns the codec declared for payloadType.
func (s *SDPInfo) GetCodecByPT(payloadType int) *CodecInfo {
	if s == nil {
		return nil
	}
	for index := range s.Codecs {
		if s.Codecs[index].PayloadType == payloadType {
			return &s.Codecs[index]
		}
	}
	return nil
}

// GetPreferredCodec returns the first codec in SDP payload order.
func (s *SDPInfo) GetPreferredCodec() *CodecInfo {
	if s == nil || len(s.Codecs) == 0 {
		return nil
	}
	return &s.Codecs[0]
}

// GetMediaAddress returns the recovered host:port media address.
func (s *SDPInfo) GetMediaAddress() string {
	if s == nil || s.ConnectionIP == "" || s.MediaPort == 0 {
		return ""
	}
	return net.JoinHostPort(s.ConnectionIP, strconv.Itoa(s.MediaPort))
}

// GetMediaPort retains the additive scalar accessor.
func (s *SDPInfo) GetMediaPort() int {
	if s == nil {
		return 0
	}
	return s.MediaPort
}

// GetMediaIPCurrent retains the displaced IP-only projection.
func (s *SDPInfo) GetMediaIPCurrent() string {
	if s == nil {
		return ""
	}
	return s.ConnectionIP
}
