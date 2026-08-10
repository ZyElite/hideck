package voice

import (
	"errors"
	"fmt"

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
	_ = relay.SetRemoteAddr(info.ConnectionIP, info.MediaPort)
	relay.Start()
	configureRelayDTMF(relay, info)
	clientSDP := callClientSDP(call)
	if len(clientSDP) == 0 {
		return RewriteSDP(raw, localIP, relay.LANPort()), nil
	}
	rewritten, mapping := RewriteSDPForClient(raw, localIP, relay.LANPort(), clientSDP)
	applyRelayPTMapping(relay, mapping)
	return rewritten, nil
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
