package voice

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"

	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/media"
)

var (
	errNilSDPCall  = errors.New("call 为空")
	errNilRTPRelay = errors.New("RTPRelay 为空，无法处理 SDP")
)

// ProcessIncomingIMSSDP applies an IMS offer/answer to the call relay and
// returns the SDP projected toward the local client.
func ProcessIncomingIMSSDP(call *Call, raw []byte, localIP string) ([]byte, error) {
	relay, err := callRTPRelay(call)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, errors.New("IMS SDP body 为空")
	}
	info, err := ParseSDP(raw)
	if err != nil || info == nil {
		return nil, fmt.Errorf("解析 IMS SDP 失败: %w", err)
	}
	if err := configureRelayDTMF(relay, info); err != nil {
		return nil, fmt.Errorf("配置 IMS DTMF 失败: %w", err)
	}
	if err := relay.ConfigureAudioCapture(audioCaptureCodecs(info)); err != nil {
		return nil, fmt.Errorf("配置通话录音失败: %w", err)
	}
	_ = relay.SetRemoteAddr(info.ConnectionIP, info.MediaPort)
	relay.Start()
	clientSDP := callClientSDP(call)
	if len(clientSDP) == 0 {
		return RewriteSDP(raw, localIP, relay.LANPort()), nil
	}
	rewritten, mapping := RewriteSDPForClient(raw, localIP, relay.LANPort(), clientSDP)
	applyRelayPTMapping(relay, mapping)
	return rewritten, nil
}

func audioCaptureCodecs(info *SDPInfo) []media.AudioCodec {
	if info == nil {
		return nil
	}
	codecs := make([]media.AudioCodec, 0, len(info.Codecs))
	for _, codec := range info.Codecs {
		codecs = append(codecs, media.AudioCodec{
			PayloadType: codec.PayloadType,
			Name:        codec.Name,
			ClockRate:   codec.ClockRate,
			Channels:    codec.Channels,
			Fmtp:        codec.Fmtp,
		})
	}
	payloadTypes := sdpAudioPayloadTypes(info.RawSDP)
	if _, offered := payloadTypes[0]; offered && info.GetCodecByPT(0) == nil {
		codecs = append(codecs, media.AudioCodec{PayloadType: 0, Name: "PCMU", ClockRate: 8000, Channels: 1})
	}
	if _, offered := payloadTypes[8]; offered && info.GetCodecByPT(8) == nil {
		codecs = append(codecs, media.AudioCodec{PayloadType: 8, Name: "PCMA", ClockRate: 8000, Channels: 1})
	}
	return codecs
}

func sdpAudioPayloadTypes(raw []byte) map[int]struct{} {
	result := make(map[int]struct{})
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("m=audio ")) {
			continue
		}
		fields := bytes.Fields(line)
		for _, field := range fields[3:] {
			payloadType, err := strconv.Atoi(string(field))
			if err == nil {
				result[payloadType] = struct{}{}
			}
		}
		break
	}
	return result
}

// ExtractAndApplyPTMapping applies dynamic IMS-to-client payload mappings to
// the active call relay.
func ExtractAndApplyPTMapping(call *Call, raw []byte) {
	if call == nil || len(raw) == 0 {
		return
	}
	relay := call.RTPRelay()
	clientSDP := callClientSDP(call)
	if relay == nil || len(clientSDP) == 0 {
		return
	}
	_, mapping := RewriteSDPForClient(raw, "0.0.0.0", 0, clientSDP)
	applyRelayPTMapping(relay, mapping)
}

// ProcessOutgoingClientSDP applies a local client endpoint to the relay and
// returns the SDP projected toward IMS.
func ProcessOutgoingClientSDP(call *Call, raw []byte, localIP string) ([]byte, error) {
	relay, err := callRTPRelay(call)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, errors.New("Client SDP body 为空")
	}
	info, err := ParseSDP(raw)
	if err == nil && info != nil {
		_ = relay.SetClientAddr(info.ConnectionIP, info.MediaPort)
	}
	return RewriteSDP(raw, localIP, relay.IMSPort()), nil
}

func callRTPRelay(call *Call) (*media.RTPRelay, error) {
	if call == nil {
		return nil, errNilSDPCall
	}
	relay := call.RTPRelay()
	if relay == nil {
		return nil, errNilRTPRelay
	}
	return relay, nil
}

func callClientSDP(call *Call) []byte {
	if call == nil {
		return nil
	}
	call.mu.RLock()
	defer call.mu.RUnlock()
	return append([]byte(nil), call.MediaState.ClientSDP...)
}

func setCallClientSDP(call *Call, raw []byte) {
	if call == nil {
		return
	}
	call.mu.Lock()
	call.MediaState.ClientSDP = append(call.MediaState.ClientSDP[:0], raw...)
	call.mu.Unlock()
}

func applyRelayPTMapping(relay *media.RTPRelay, mapping map[int]int) {
	for imsPayloadType, clientPayloadType := range mapping {
		relay.SetPTMapping(imsPayloadType, clientPayloadType)
	}
}

// ProcessIncomingIMSSDPCurrent retains the displaced parser-only helper.
func ProcessIncomingIMSSDPCurrent(raw string) (*SDPInfoCurrent, error) {
	return ParseSDPCurrent(raw)
}

// ProcessOutgoingClientSDPCurrent retains the displaced parser-only helper.
func ProcessOutgoingClientSDPCurrent(raw string) (*SDPInfoCurrent, error) {
	return ParseSDPCurrent(raw)
}
