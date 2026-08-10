package voice

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/media"
)

const clientRelayIP = "127.0.0.1"

type imsPacketListener interface {
	ListenPacket(network string, addr *net.UDPAddr) (net.PacketConn, error)
}

type imscorePacketListener struct{ service *imscore.Service }

func (listener imscorePacketListener) ListenPacket(
	network string,
	address *net.UDPAddr,
) (net.PacketConn, error) {
	return listener.service.ListenPacketCurrent(network, address)
}

type mediaPacketListenerAdapter struct{ current imsPacketListener }

func (adapter mediaPacketListenerAdapter) ListenPacket(
	_ context.Context,
	network string,
	address net.Addr,
) (net.PacketConn, error) {
	udpAddress, ok := address.(*net.UDPAddr)
	if !ok {
		return nil, fmt.Errorf("voice: IMS media address is %T, want UDP", address)
	}
	return adapter.current.ListenPacket(network, udpAddress)
}

var _ imsendpoint.PacketListener = mediaPacketListenerAdapter{}

func (a *Agent) newEndpointMediaRelay(localIP string) (*media.RTPRelay, error) {
	endpoint := a.imsEndpoint()
	listener, ok := endpoint.(imsendpoint.PacketListener)
	if !ok {
		return nil, errors.New("voice: IMS endpoint does not expose packet transport")
	}
	if net.ParseIP(strings.TrimSpace(localIP)) == nil {
		return nil, fmt.Errorf("voice: invalid IMS media IP %q", localIP)
	}
	return media.NewRTPRelayWithListener(listener, localIP, clientRelayIP, 0, 0)
}

func newVoiceMediaRelay(imsNetwork imsPacketListener, imsLocalIP string) (*media.RTPRelay, error) {
	if imsNetwork == nil {
		return nil, errors.New("voice: IMS media network is unavailable")
	}
	if net.ParseIP(strings.TrimSpace(imsLocalIP)) == nil {
		return nil, fmt.Errorf("voice: invalid IMS media IP %q", imsLocalIP)
	}
	return media.NewRTPRelayWithListener(
		mediaPacketListenerAdapter{current: imsNetwork},
		imsLocalIP, clientRelayIP, 0, 0,
	)
}

func (a *Agent) prepareInboundMedia(call *Call, offer string) error {
	if err := validateSDPMediaEndpoint([]byte(offer), "IMS offer"); err != nil {
		return err
	}
	if a.newMediaRelay == nil {
		return errors.New("voice: media relay factory is unavailable")
	}
	relay, err := a.newMediaRelay(a.localIP())
	if err != nil {
		return err
	}
	call.SetRTPRelay(relay)
	rewritten, err := ProcessIncomingIMSSDP(call, []byte(offer), clientRelayIP)
	if err != nil {
		relay.Stop()
		return err
	}
	call.setRemoteSDP(offer, string(rewritten))
	return nil
}

func (a *Agent) applyInboundAnswer(call *Call, answer string) (InboundAnswer, error) {
	if err := validateSDPMediaEndpoint([]byte(answer), "client answer"); err != nil {
		return InboundAnswer{}, err
	}
	relay := call.RTPRelay()
	if relay == nil || relay.IMSPort() <= 0 || relay.LANPort() <= 0 {
		return InboundAnswer{}, errors.New("voice: inbound media relay is unavailable")
	}
	setCallClientSDP(call, []byte(answer))
	imsAnswer, err := ProcessOutgoingClientSDP(call, []byte(answer), a.localIP())
	if err != nil {
		return InboundAnswer{}, err
	}
	ExtractAndApplyPTMapping(call, []byte(call.remoteSDPValue()))
	call.setLocalSDP(answer, string(imsAnswer))
	return InboundAnswer{CallID: call.CallID(), OfferSDP: call.clientRemoteSDPValue(), State: call.CallState().String()}, nil
}

func (a *Agent) prepareOutboundMedia(call *Call, clientOffer string) (string, error) {
	if strings.TrimSpace(clientOffer) == "" {
		return a.prepareSimulatedOutboundMedia(call)
	}
	if err := validateSDPMediaEndpoint([]byte(clientOffer), "client offer"); err != nil {
		return "", err
	}
	relay, err := a.newOutboundMediaRelay()
	if err != nil {
		return "", err
	}
	call.SetRTPRelay(relay)
	setCallClientSDP(call, []byte(clientOffer))
	imsOffer, err := ProcessOutgoingClientSDP(call, []byte(clientOffer), a.localIP())
	if err != nil {
		relay.Stop()
		return "", err
	}
	call.setLocalSDP(clientOffer, string(imsOffer))
	return string(imsOffer), nil
}

func (a *Agent) prepareSimulatedOutboundMedia(call *Call) (string, error) {
	relay, err := a.newOutboundMediaRelay()
	if err != nil {
		return "", err
	}
	call.SetRTPRelay(relay)
	imsOffer := generateBasicSDPCurrent(a, call)
	if strings.TrimSpace(imsOffer) == "" {
		relay.Stop()
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
	response = finalOrEarlyMediaResponse(call, response)
	if !isVoiceSDPContentType(voiceResponseHeader(response.Headers, "Content-Type")) {
		return errors.New("voice: IMS INVITE response has no application/sdp media answer")
	}
	if err := validateSDPMediaEndpoint(response.Body, "IMS answer"); err != nil {
		return err
	}
	relay := call.RTPRelay()
	if relay == nil {
		return errors.New("voice: outbound media relay is unavailable")
	}
	clientOffer, _ := call.localSDPs()
	clientAnswer, err := ProcessIncomingIMSSDP(call, response.Body, clientRelayIP)
	if err != nil {
		return err
	}
	if strings.TrimSpace(clientOffer) == "" {
		return completeSimulatedOutboundMedia(a, call, string(response.Body))
	}
	call.setRemoteSDP(string(response.Body), string(clientAnswer))
	a.enableMediaMonitor(call)
	return nil
}

func finalOrEarlyMediaResponse(call *Call, response imscore.SIPResponse) imscore.SIPResponse {
	if isVoiceSDPContentType(voiceResponseHeader(response.Headers, "Content-Type")) && len(response.Body) > 0 {
		return response
	}
	if call == nil {
		return response
	}
	earlyAnswer := call.remoteSDPValue()
	if strings.TrimSpace(earlyAnswer) == "" {
		return response
	}
	response.Headers = map[string]string{"Content-Type": "application/sdp"}
	response.Body = []byte(earlyAnswer)
	return response
}

func completeSimulatedOutboundMedia(a *Agent, call *Call, rawAnswer string) error {
	call.setComfortNoise(media.NewComfortNoiseGenerator())
	call.setRemoteSDP(rawAnswer, "")
	a.enableMediaMonitor(call)
	return call.startMediaResourcesCurrent()
}

func (a *Agent) updateRemoteMedia(call *Call, response imscore.SIPResponse) error {
	if !isVoiceSDPContentType(voiceResponseHeader(response.Headers, "Content-Type")) {
		return errors.New("voice: session refresh response has no application/sdp media answer")
	}
	if err := validateSDPMediaEndpoint(response.Body, "IMS session refresh"); err != nil {
		return err
	}
	remoteSDP := string(response.Body)
	relay := call.RTPRelay()
	if relay == nil {
		return errors.New("voice: session refresh media relay is unavailable")
	}
	clientLocal, _ := call.localSDPs()
	clientRemote, err := ProcessIncomingIMSSDP(call, response.Body, clientRelayIP)
	if err != nil {
		return err
	}
	if strings.TrimSpace(clientLocal) == "" {
		call.setRemoteSDP(remoteSDP, "")
		return nil
	}
	call.setRemoteSDP(remoteSDP, string(clientRemote))
	return nil
}

func configureRelayDTMF(relay *media.RTPRelay, info *SDPInfo) {
	if relay == nil || info == nil {
		return
	}
	for _, codec := range info.Codecs {
		if strings.EqualFold(codec.Name, sdpTelephoneEvent) {
			_ = relay.SetDTMFPayloadType(codec.PayloadType)
			return
		}
	}
}
