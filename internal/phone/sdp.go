package phone

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type rtpEndpoint struct {
	Address     *net.UDPAddr
	Codec       string
	PayloadType uint8
}

func plainAudioSDP(port int) string {
	return fmt.Sprintf("v=0\r\no=vohive 0 0 IN IP4 127.0.0.1\r\ns=VoHive Phone\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP 0 8 101\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:8 PCMA/8000\r\na=rtpmap:101 telephone-event/8000\r\na=fmtp:101 0-15\r\na=ptime:20\r\na=sendrecv\r\n", port)
}

func parseRTPEndpoint(raw string) (rtpEndpoint, error) {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	host := ""
	port, payloadTypes := 0, []int(nil)
	codecs := make(map[int]string)
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		switch {
		case len(fields) >= 3 && strings.HasPrefix(line, "c="):
			host = fields[len(fields)-1]
		case len(fields) >= 4 && strings.HasPrefix(line, "m=audio"):
			port, _ = strconv.Atoi(fields[1])
			for _, field := range fields[3:] {
				if value, err := strconv.Atoi(field); err == nil {
					payloadTypes = append(payloadTypes, value)
				}
			}
		case strings.HasPrefix(line, "a=rtpmap:"):
			parseRTPMap(line, codecs)
		}
	}
	if host == "" || port <= 0 {
		return rtpEndpoint{}, errors.New("phone: SDP has no usable RTP endpoint")
	}
	return selectRTPEndpoint(host, port, payloadTypes, codecs)
}

func parseRTPMap(line string, codecs map[int]string) {
	value := strings.TrimPrefix(strings.TrimSpace(line), "a=rtpmap:")
	parts := strings.Fields(value)
	if len(parts) != 2 {
		return
	}
	payloadType, err := strconv.Atoi(parts[0])
	if err != nil {
		return
	}
	codecs[payloadType] = strings.ToUpper(strings.SplitN(parts[1], "/", 2)[0])
}

func selectRTPEndpoint(host string, port int, payloadTypes []int, codecs map[int]string) (rtpEndpoint, error) {
	unavailableCodec := ""
	for _, payloadType := range payloadTypes {
		codec := codecs[payloadType]
		if payloadType == 0 && codec == "" {
			codec = "PCMU"
		}
		if payloadType == 8 && codec == "" {
			codec = "PCMA"
		}
		if codec == "AMR" || codec == "AMR-WB" {
			unavailableCodec = codec
		}
		if codec != "PCMU" && codec != "PCMA" {
			continue
		}
		address, err := net.ResolveUDPAddr("udp", net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port)))
		if err != nil {
			return rtpEndpoint{}, fmt.Errorf("phone: resolve RTP endpoint: %w", err)
		}
		return rtpEndpoint{Address: address, Codec: codec, PayloadType: uint8(payloadType)}, nil
	}
	if unavailableCodec != "" {
		return rtpEndpoint{}, fmt.Errorf("phone: negotiated %s codec requires an unavailable encoder", unavailableCodec)
	}
	return rtpEndpoint{}, errors.New("phone: negotiated RTP audio codec is unsupported")
}
