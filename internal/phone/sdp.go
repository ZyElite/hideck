package phone

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

const (
	amrWBPayloadType = 104
	amrPayloadType   = 114
	dtmfPayloadType  = 101
)

type rtpEndpoint struct {
	Address     *net.UDPAddr
	Codec       string
	PayloadType uint8
	ClockRate   int
	Fmtp        string
}

type rtpCodec struct {
	name      string
	clockRate int
}

type endpointSelection struct {
	host         string
	port         int
	payloadTypes []int
	codecs       map[int]rtpCodec
	fmtps        map[int]string
	supported    []string
}

func plainAudioSDP(port int, realtimeCodecs []string) string {
	payloads, attributes := advertisedAudioCodecs(realtimeCodecs)
	return renderPlainAudioSDP(port, payloads, attributes)
}

func plainSelectedAudioSDP(port int, endpoint rtpEndpoint) string {
	payload := strconv.Itoa(int(endpoint.PayloadType))
	attribute := fmt.Sprintf("a=rtpmap:%s %s/%d", payload, endpoint.Codec, endpoint.ClockRate)
	attributes := []string{attribute}
	if endpoint.Fmtp != "" {
		attributes = append(attributes, "a=fmtp:"+payload+" "+endpoint.Fmtp)
	}
	return renderPlainAudioSDP(port, []string{payload, strconv.Itoa(dtmfPayloadType)}, attributes)
}

func renderPlainAudioSDP(port int, payloads, attributes []string) string {
	return fmt.Sprintf(
		"v=0\r\no=vohive 0 0 IN IP4 127.0.0.1\r\ns=VoHive Phone\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP %s\r\n%s\r\na=rtpmap:%d telephone-event/8000\r\na=fmtp:%d 0-15\r\na=ptime:20\r\na=sendrecv\r\n",
		port, strings.Join(payloads, " "), strings.Join(attributes, "\r\n"), dtmfPayloadType, dtmfPayloadType,
	)
}

func advertisedAudioCodecs(realtimeCodecs []string) ([]string, []string) {
	payloads := make([]string, 0, len(realtimeCodecs)+3)
	attributes := make([]string, 0, len(realtimeCodecs)*2+2)
	seen := make(map[string]struct{})
	for _, codec := range realtimeCodecs {
		codec = strings.ToUpper(strings.TrimSpace(codec))
		if _, duplicate := seen[codec]; duplicate {
			continue
		}
		seen[codec] = struct{}{}
		switch codec {
		case "AMR-WB":
			payloads = append(payloads, strconv.Itoa(amrWBPayloadType))
			attributes = append(attributes,
				fmt.Sprintf("a=rtpmap:%d AMR-WB/16000", amrWBPayloadType),
				fmt.Sprintf("a=fmtp:%d octet-align=1; max-red=0", amrWBPayloadType),
			)
		case "AMR":
			payloads = append(payloads, strconv.Itoa(amrPayloadType))
			attributes = append(attributes,
				fmt.Sprintf("a=rtpmap:%d AMR/8000", amrPayloadType),
				fmt.Sprintf("a=fmtp:%d octet-align=1; max-red=0", amrPayloadType),
			)
		}
	}
	payloads = append(payloads, "0", "8", strconv.Itoa(dtmfPayloadType))
	attributes = append(attributes, "a=rtpmap:0 PCMU/8000", "a=rtpmap:8 PCMA/8000")
	return payloads, attributes
}

func parseRTPEndpoint(raw string, supported ...string) (rtpEndpoint, error) {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	host, port := "", 0
	var payloadTypes []int
	codecs, fmtps := make(map[int]rtpCodec), make(map[int]string)
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		switch {
		case len(fields) >= 3 && strings.HasPrefix(line, "c="):
			host = fields[len(fields)-1]
		case len(fields) >= 4 && strings.HasPrefix(line, "m=audio"):
			port, _ = strconv.Atoi(fields[1])
			payloadTypes = appendPayloadTypes(payloadTypes, fields[3:])
		case strings.HasPrefix(line, "a=rtpmap:"):
			parseRTPMap(line, codecs)
		case strings.HasPrefix(line, "a=fmtp:"):
			parseEndpointFMTP(line, fmtps)
		}
	}
	if host == "" || port <= 0 {
		return rtpEndpoint{}, errors.New("phone: SDP has no usable RTP endpoint")
	}
	return selectRTPEndpoint(endpointSelection{
		host: host, port: port, payloadTypes: payloadTypes,
		codecs: codecs, fmtps: fmtps, supported: supported,
	})
}

func appendPayloadTypes(destination []int, fields []string) []int {
	for _, field := range fields {
		if value, err := strconv.Atoi(field); err == nil {
			destination = append(destination, value)
		}
	}
	return destination
}

func parseRTPMap(line string, codecs map[int]rtpCodec) {
	parts := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "a=rtpmap:"))
	if len(parts) != 2 {
		return
	}
	payloadType, err := strconv.Atoi(parts[0])
	encoding := strings.Split(parts[1], "/")
	if err != nil || len(encoding) < 2 {
		return
	}
	clockRate, err := strconv.Atoi(encoding[1])
	if err == nil {
		codecs[payloadType] = rtpCodec{name: strings.ToUpper(encoding[0]), clockRate: clockRate}
	}
}

func parseEndpointFMTP(line string, fmtps map[int]string) {
	parts := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "a=fmtp:"))
	if len(parts) < 2 {
		return
	}
	payloadType, err := strconv.Atoi(parts[0])
	if err == nil {
		fmtps[payloadType] = strings.Join(parts[1:], " ")
	}
}

func selectRTPEndpoint(selection endpointSelection) (rtpEndpoint, error) {
	allowed := supportedCodecSet(selection.supported)
	unavailableAMR := ""
	for _, payloadType := range selection.payloadTypes {
		codec := selection.codecs[payloadType]
		if payloadType == 0 && codec.name == "" {
			codec = rtpCodec{name: "PCMU", clockRate: 8000}
		}
		if payloadType == 8 && codec.name == "" {
			codec = rtpCodec{name: "PCMA", clockRate: 8000}
		}
		if codec.name != "PCMU" && codec.name != "PCMA" && codec.name != "AMR" && codec.name != "AMR-WB" {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[codec.name]; !ok {
				if codec.name == "AMR" || codec.name == "AMR-WB" {
					unavailableAMR = codec.name
				}
				continue
			}
		}
		address, err := net.ResolveUDPAddr("udp", net.JoinHostPort(strings.Trim(selection.host, "[]"), strconv.Itoa(selection.port)))
		if err != nil {
			return rtpEndpoint{}, fmt.Errorf("phone: resolve RTP endpoint: %w", err)
		}
		return rtpEndpoint{
			Address: address, Codec: codec.name, PayloadType: uint8(payloadType),
			ClockRate: codec.clockRate, Fmtp: selection.fmtps[payloadType],
		}, nil
	}
	if unavailableAMR != "" {
		return rtpEndpoint{}, fmt.Errorf("phone: negotiated %s codec requires an unavailable encoder", unavailableAMR)
	}
	return rtpEndpoint{}, errors.New("phone: negotiated RTP audio codec is unsupported")
}

func supportedCodecSet(codecs []string) map[string]struct{} {
	result := make(map[string]struct{}, len(codecs))
	for _, codec := range codecs {
		if codec = strings.ToUpper(strings.TrimSpace(codec)); codec != "" {
			result[codec] = struct{}{}
		}
	}
	return result
}
