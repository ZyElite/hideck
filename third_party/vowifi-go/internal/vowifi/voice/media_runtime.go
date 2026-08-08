package voice

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/media"
)

const clientRelayIP = "127.0.0.1"

type imsPacketListener interface {
	ListenPacket(network string, addr *net.UDPAddr) (net.PacketConn, error)
}

func newVoiceMediaRelay(imsNetwork imsPacketListener, imsLocalIP string) (*media.RTPRelay, error) {
	if imsNetwork == nil {
		return nil, errors.New("voice: IMS media network is unavailable")
	}
	bindIP := net.ParseIP(strings.TrimSpace(imsLocalIP))
	if bindIP == nil {
		return nil, fmt.Errorf("voice: invalid IMS media IP %q", imsLocalIP)
	}
	imsConn, err := imsNetwork.ListenPacket("udp", &net.UDPAddr{IP: bindIP})
	if err != nil {
		return nil, fmt.Errorf("voice: listen IMS RTP: %w", err)
	}
	lanConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		_ = imsConn.Close()
		return nil, fmt.Errorf("voice: listen client RTP: %w", err)
	}
	return media.NewRTPRelay(imsConn, lanConn), nil
}

func mediaRemote(info *SDPInfo) (*net.UDPAddr, error) {
	if info == nil || info.GetMediaPort() <= 0 {
		return nil, errors.New("voice: SDP media port must be greater than zero")
	}
	ip := net.ParseIP(strings.TrimSpace(info.GetMediaAddress()))
	if ip == nil {
		return nil, fmt.Errorf("voice: invalid SDP media address %q", info.GetMediaAddress())
	}
	return &net.UDPAddr{IP: ip, Port: info.GetMediaPort()}, nil
}

func (a *Agent) prepareInboundMedia(call *Call, offer string) error {
	parsed, err := ProcessIncomingIMSSDP(offer)
	if err != nil {
		return err
	}
	remote, err := mediaRemote(parsed)
	if err != nil {
		return err
	}
	if a.newMediaRelay == nil {
		return errors.New("voice: media relay factory is unavailable")
	}
	relay, err := a.newMediaRelay(a.localIP())
	if err != nil {
		return err
	}
	relay.SetRemoteAddr(remote)
	call.SetRTPRelay(relay)
	call.setRemoteSDP(offer, RewriteSDP(offer, clientRelayIP, relay.LANPort()))
	return nil
}

func (a *Agent) applyInboundAnswer(call *Call, answer string) (InboundAnswer, error) {
	parsedAnswer, err := ProcessOutgoingClientSDP(answer)
	if err != nil {
		return InboundAnswer{}, err
	}
	clientRemote, err := mediaRemote(parsedAnswer)
	if err != nil {
		return InboundAnswer{}, err
	}
	relay := call.RTPRelay()
	if relay == nil || relay.IMSPort() <= 0 || relay.LANPort() <= 0 {
		return InboundAnswer{}, errors.New("voice: inbound media relay is unavailable")
	}
	remoteOffer, err := ProcessIncomingIMSSDP(call.remoteSDPValue())
	if err != nil {
		return InboundAnswer{}, err
	}
	relay.SetClientAddr(clientRemote)
	relay.SetPTMapping(ExtractAndApplyPTMapping(remoteOffer, parsedAnswer))
	imsAnswer := RewriteSDP(answer, a.localIP(), relay.IMSPort())
	call.setLocalSDP(answer, imsAnswer)
	return InboundAnswer{CallID: call.CallID(), OfferSDP: call.clientRemoteSDPValue(), State: call.GetState().String()}, nil
}

func (a *Agent) prepareOutboundMedia(call *Call, clientOffer string) (string, error) {
	if strings.TrimSpace(clientOffer) == "" {
		return a.prepareSimulatedOutboundMedia(call)
	}
	parsedOffer, err := ProcessOutgoingClientSDP(clientOffer)
	if err != nil {
		return "", err
	}
	clientRemote, err := mediaRemote(parsedOffer)
	if err != nil {
		return "", err
	}
	relay, err := a.newOutboundMediaRelay()
	if err != nil {
		return "", err
	}
	relay.SetClientAddr(clientRemote)
	call.SetRTPRelay(relay)
	imsOffer := RewriteSDP(clientOffer, a.localIP(), relay.IMSPort())
	call.setLocalSDP(clientOffer, imsOffer)
	return imsOffer, nil
}

func (a *Agent) prepareSimulatedOutboundMedia(call *Call) (string, error) {
	relay, err := a.newOutboundMediaRelay()
	if err != nil {
		return "", err
	}
	call.SetRTPRelay(relay)
	imsOffer := generateBasicSDP(a, call)
	if strings.TrimSpace(imsOffer) == "" {
		_ = relay.Stop()
		return "", errors.New("voice: failed to generate simulated-call SDP")
	}
	call.setLocalSDP("", imsOffer)
	return imsOffer, nil
}

func (a *Agent) newOutboundMediaRelay() (*media.RTPRelay, error) {
	if a.newMediaRelay == nil {
		return nil, errors.New("voice: media relay factory is unavailable")
	}
	return a.newMediaRelay(a.localIP())
}

func (a *Agent) completeOutboundMedia(call *Call, response imscore.SIPResponse) error {
	if !isVoiceSDPContentType(voiceResponseHeader(response.Headers, "Content-Type")) {
		return errors.New("voice: IMS INVITE response has no application/sdp media answer")
	}
	imsAnswer, err := ProcessIncomingIMSSDP(string(response.Body))
	if err != nil {
		return err
	}
	remote, err := mediaRemote(imsAnswer)
	if err != nil {
		return err
	}
	relay := call.RTPRelay()
	if relay == nil {
		return errors.New("voice: outbound media relay is unavailable")
	}
	clientOffer, _ := call.localSDPs()
	relay.SetRemoteAddr(remote)
	if strings.TrimSpace(clientOffer) == "" {
		return completeSimulatedOutboundMedia(call, imsAnswer, string(response.Body))
	}
	parsedClientOffer, err := ProcessOutgoingClientSDP(clientOffer)
	if err != nil {
		return err
	}
	relay.SetPTMapping(ExtractAndApplyPTMapping(imsAnswer, parsedClientOffer))
	clientAnswer := RewriteSDP(string(response.Body), clientRelayIP, relay.LANPort())
	call.setRemoteSDP(string(response.Body), clientAnswer)
	return call.StartMedia()
}

func completeSimulatedOutboundMedia(call *Call, answer *SDPInfo, rawAnswer string) error {
	if err := requirePCMU(answer); err != nil {
		return err
	}
	call.setComfortNoise(media.NewComfortNoiseGenerator())
	call.setRemoteSDP(rawAnswer, "")
	return call.StartMedia()
}

func requirePCMU(answer *SDPInfo) error {
	if answer == nil {
		return errors.New("voice: empty simulated-call SDP answer")
	}
	for _, section := range answer.Media {
		if section.Type != "audio" {
			continue
		}
		for _, codec := range section.Codecs {
			if codec.PayloadType == 0 && strings.EqualFold(codec.Encoding, "PCMU") && codec.ClockRate == 8000 {
				return nil
			}
		}
		for _, payloadType := range section.Formats {
			if payloadType == 0 {
				return nil
			}
		}
	}
	return errors.New("voice: IMS SDP answer does not accept PT0 PCMU media")
}

func (a *Agent) updateRemoteMedia(call *Call, response imscore.SIPResponse) error {
	if !isVoiceSDPContentType(voiceResponseHeader(response.Headers, "Content-Type")) {
		return errors.New("voice: session refresh response has no application/sdp media answer")
	}
	remoteSDP := string(response.Body)
	parsedRemote, err := ProcessIncomingIMSSDP(remoteSDP)
	if err != nil {
		return err
	}
	remote, err := mediaRemote(parsedRemote)
	if err != nil {
		return err
	}
	relay := call.RTPRelay()
	if relay == nil {
		return errors.New("voice: session refresh media relay is unavailable")
	}
	clientLocal, _ := call.localSDPs()
	if strings.TrimSpace(clientLocal) == "" {
		if err := requirePCMU(parsedRemote); err != nil {
			return err
		}
		relay.SetRemoteAddr(remote)
		call.setRemoteSDP(remoteSDP, "")
		return nil
	}
	parsedClient, err := ProcessOutgoingClientSDP(clientLocal)
	if err != nil {
		return err
	}
	relay.SetRemoteAddr(remote)
	relay.SetPTMapping(ExtractAndApplyPTMapping(parsedRemote, parsedClient))
	call.setRemoteSDP(remoteSDP, RewriteSDP(remoteSDP, clientRelayIP, relay.LANPort()))
	return nil
}
