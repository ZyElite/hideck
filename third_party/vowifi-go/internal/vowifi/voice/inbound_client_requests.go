package voice

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/client"
)

type clientInviteBuildConfig struct {
	recipient sip.Uri
	localIP   string
	username  string
	fromTag   string
}

type clientDialogHeaderConfig struct {
	method sip.RequestMethod
	cseq   uint32
}

func (a *Agent) buildClientInviteReq(
	call *Call,
	bridge *client.Bridge,
	target inboundClientTarget,
) (*sip.Request, error) {
	if a == nil || call == nil || bridge == nil {
		return nil, errors.New("voice: client bridge is unavailable")
	}
	var recipient sip.Uri
	if err := sip.ParseUri(strings.TrimSpace(target.contactURI), &recipient); err != nil {
		return nil, fmt.Errorf("voice: parse client Contact: %w", err)
	}
	if target.username = strings.TrimSpace(target.username); target.username != "" {
		recipient.User = target.username
	}
	destination, err := clientDestination(recipient, target.destination)
	if err != nil {
		return nil, err
	}
	localIP := strings.TrimSpace(bridge.LocalIP())
	if localIP == "" {
		return nil, errors.New("voice: client bridge local IP is unavailable")
	}
	fromTag := voiceTag()
	request := buildClientInviteMessage(call, bridge, clientInviteBuildConfig{
		recipient: recipient, localIP: localIP, username: target.username, fromTag: fromTag,
	})
	request.SetDestination(destination)
	request.SetTransport("UDP")
	call.storeClientRequestContext(clientRequestContext{
		request: request, destination: destination, localIP: localIP, fromTag: fromTag,
	})
	return request, nil
}

func buildClientInviteMessage(
	call *Call,
	bridge *client.Bridge,
	cfg clientInviteBuildConfig,
) *sip.Request {
	request := sip.NewRequest(sip.INVITE, cfg.recipient)
	fromParams := sip.NewParams()
	fromParams.Add("tag", cfg.fromTag)
	request.AppendHeader(&sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: call.CallerID, Host: cfg.localIP}, Params: fromParams})
	request.AppendHeader(&sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: cfg.username, Host: cfg.localIP}, Params: sip.NewParams()})
	callID := sip.CallIDHeader(call.ClientCallID())
	request.AppendHeader(&callID)
	request.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.INVITE})
	maxForwards := sip.MaxForwardsHeader(70)
	request.AppendHeader(&maxForwards)
	host, port := bridge.ListenHostPort()
	request.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "vohive", Host: host, Port: port}})
	if call.agent != nil && call.agent.dialog != nil {
		if userAgent := strings.TrimSpace(call.agent.dialog.UserAgent()); userAgent != "" {
			request.AppendHeader(sip.NewHeader("User-Agent", userAgent))
		}
	}
	request.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	request.SetBody([]byte(call.ClientSDP()))
	return request
}

func clientDestination(recipient sip.Uri, configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		if _, _, err := net.SplitHostPort(configured); err == nil {
			return configured, nil
		}
		if net.ParseIP(strings.Trim(configured, "[]")) != nil || validClientHost(configured) {
			return net.JoinHostPort(strings.Trim(configured, "[]"), "5060"), nil
		}
		return "", fmt.Errorf("voice: invalid client destination %q", configured)
	}
	if strings.TrimSpace(recipient.Host) == "" {
		return "", errors.New("voice: client Contact host is empty")
	}
	port := recipient.Port
	if port == 0 {
		port = 5060
	}
	return net.JoinHostPort(recipient.Host, fmt.Sprint(port)), nil
}

func validClientHost(value string) bool {
	return !strings.ContainsAny(value, " :/\\@") && strings.TrimSpace(value) != ""
}

func (a *Agent) sendClientACK(call *Call) error {
	invite, response := call.clientDialogContext()
	if invite == nil || response == nil {
		return errors.New("voice: client dialog context is unavailable")
	}
	request := buildClientDialogRequest(sip.ACK, invite, response, invite.CSeq().SeqNo)
	return writeInboundClientRequest(call, "inbound_client_ack", request)
}

func (a *Agent) sendClientCancel(call *Call) error {
	return sendClientCancelRequest(call.inboundBridge(), call.takeClientCancelRequest())
}

func sendClientCancelRequest(bridge *client.Bridge, invite *sip.Request) error {
	if invite == nil {
		return nil
	}
	if bridge == nil {
		return errors.New("voice: client bridge is unavailable")
	}
	request, err := sipkit.BuildCancelFromInvite(invite)
	if err != nil {
		return err
	}
	return bridge.WriteRequest(context.Background(), "inbound_client_cancel", request)
}

func (a *Agent) sendClientBye(call *Call) error {
	invite, response := call.takeClientByeContext()
	if invite == nil || response == nil {
		return nil
	}
	request := buildClientDialogRequest(sip.BYE, invite, response, invite.CSeq().SeqNo+1)
	return writeInboundClientRequest(call, "inbound_client_bye", request)
}

func writeInboundClientRequest(call *Call, flow string, request *sip.Request) error {
	bridge := call.inboundBridge()
	if bridge == nil {
		return errors.New("voice: client bridge is unavailable")
	}
	return bridge.WriteRequest(context.Background(), flow, request)
}

func buildClientDialogRequest(
	method sip.RequestMethod,
	invite *sip.Request,
	response *sip.Response,
	cseq uint32,
) *sip.Request {
	recipient := invite.Recipient
	if contact := response.Contact(); contact != nil {
		recipient = *contact.Address.Clone()
	}
	request := sip.NewRequest(method, recipient)
	appendClientDialogHeaders(request, invite, response, clientDialogHeaderConfig{
		method: method, cseq: cseq,
	})
	request.SetDestination(invite.Destination())
	request.SetTransport(invite.Transport())
	return request
}

func appendClientDialogHeaders(
	request *sip.Request,
	invite *sip.Request,
	response *sip.Response,
	cfg clientDialogHeaderConfig,
) {
	maxForwards := sip.MaxForwardsHeader(70)
	request.AppendHeader(&maxForwards)
	if header := invite.From(); header != nil {
		request.AppendHeader(sip.HeaderClone(header))
	}
	if header := response.To(); header != nil {
		request.AppendHeader(sip.HeaderClone(header))
	}
	if header := invite.CallID(); header != nil {
		request.AppendHeader(sip.HeaderClone(header))
	}
	request.AppendHeader(&sip.CSeqHeader{SeqNo: cfg.cseq, MethodName: cfg.method})
	if header := invite.Contact(); header != nil {
		request.AppendHeader(sip.HeaderClone(header))
	}
	for _, header := range response.GetHeaders("Record-Route") {
		request.PrependHeader(sip.NewHeader("Route", header.Value()))
	}
}
