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

func newVoiceMediaRelay(imsLocalIP string) (*media.RTPRelay, error) {
	bindIP := net.ParseIP(strings.TrimSpace(imsLocalIP))
	if bindIP == nil || bindIP.To4() == nil {
		return nil, fmt.Errorf("voice: invalid IMS media IP %q", imsLocalIP)
	}
	imsConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: bindIP})
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
	if ip == nil || ip.To4() == nil {
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
	parsedOffer, err := ProcessOutgoingClientSDP(clientOffer)
	if err != nil {
		return "", err
	}
	clientRemote, err := mediaRemote(parsedOffer)
	if err != nil {
		return "", err
	}
	if a.newMediaRelay == nil {
		return "", errors.New("voice: media relay factory is unavailable")
	}
	relay, err := a.newMediaRelay(a.localIP())
	if err != nil {
		return "", err
	}
	relay.SetClientAddr(clientRemote)
	call.SetRTPRelay(relay)
	imsOffer := RewriteSDP(clientOffer, a.localIP(), relay.IMSPort())
	call.setLocalSDP(clientOffer, imsOffer)
	return imsOffer, nil
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
	parsedClientOffer, err := ProcessOutgoingClientSDP(clientOffer)
	if err != nil {
		return err
	}
	relay.SetRemoteAddr(remote)
	relay.SetPTMapping(ExtractAndApplyPTMapping(imsAnswer, parsedClientOffer))
	clientAnswer := RewriteSDP(string(response.Body), clientRelayIP, relay.LANPort())
	call.setRemoteSDP(string(response.Body), clientAnswer)
	return call.StartMedia()
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
	clientLocal, _ := call.localSDPs()
	parsedClient, err := ProcessOutgoingClientSDP(clientLocal)
	if err != nil {
		return err
	}
	if relay == nil {
		return errors.New("voice: session refresh media relay is unavailable")
	}
	relay.SetRemoteAddr(remote)
	relay.SetPTMapping(ExtractAndApplyPTMapping(parsedRemote, parsedClient))
	call.setRemoteSDP(remoteSDP, RewriteSDP(remoteSDP, clientRelayIP, relay.LANPort()))
	return nil
}
