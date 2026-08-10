package voice

import (
	"fmt"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
)

func (a *Agent) forwardResponseToClient(call *Call, source imscore.SIPResponse) error {
	if call == nil || source.StatusCode <= 100 {
		return nil
	}
	call.mu.RLock()
	request := call.DialogState.OriginalRequest
	transaction := call.DialogState.ClientTx
	call.mu.RUnlock()
	if request == nil {
		return nil
	}
	if source.StatusCode >= 200 && !call.markClientFinalSent() {
		return nil
	}
	response := buildClientResponseFromRequest(
		request, source.StatusCode, source.Reason, clientResponseBody(call, source),
	)
	applyClientResponseHeaders(response, call, source)
	return a.respondClientWithFallback(transaction, response)
}

func clientResponseBody(call *Call, source imscore.SIPResponse) []byte {
	if !isVoiceSDPContentType(voiceResponseHeader(source.Headers, "Content-Type")) {
		return append([]byte(nil), source.Body...)
	}
	if rewritten := strings.TrimSpace(call.ClientSDP()); rewritten != "" {
		return []byte(rewritten)
	}
	return append([]byte(nil), source.Body...)
}

func applyClientResponseHeaders(response *sip.Response, call *Call, source imscore.SIPResponse) {
	if response == nil {
		return
	}
	applyClientToTag(response, call, voiceHeaderTag(voiceResponseHeader(source.Headers, "To")))
	applyClientContact(response, call)
	copyClientResponseHeader(response, source.Headers, "Supported")
	copyClientResponseHeader(response, source.Headers, "Allow")
	copyClientResponseHeader(response, source.Headers, "Require")
	copyClientResponseHeader(response, source.Headers, "RSeq")
	if len(response.Body()) > 0 {
		response.RemoveHeader("Content-Type")
		response.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
		response.SetBody(response.Body())
	}
}

func applyClientToTag(response *sip.Response, call *Call, tag string) {
	if response.To() == nil || strings.TrimSpace(tag) == "" {
		return
	}
	response.To().Params.Remove("tag")
	response.To().Params.Add("tag", tag)
	call.mu.Lock()
	call.DialogState.ClientToTag = tag
	call.mu.Unlock()
}

func applyClientContact(response *sip.Response, call *Call) {
	if call == nil || call.agent == nil || call.agent.clientBridge == nil || response.StatusCode <= 100 {
		return
	}
	host, port := call.agent.clientBridge.ListenHostPort()
	if strings.TrimSpace(host) == "" || port <= 0 {
		return
	}
	response.RemoveHeader("Contact")
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{
		Scheme: "sip", User: "vohive", Host: host, Port: port,
	}})
}

func copyClientResponseHeader(response *sip.Response, headers map[string]string, name string) {
	value := voiceResponseHeader(headers, name)
	if value == "" {
		return
	}
	response.RemoveHeader(name)
	response.AppendHeader(sip.NewHeader(name, value))
}

func (a *Agent) sendResponseViaFallback(response *sip.Response) error {
	if response == nil {
		return fmt.Errorf("voice: client response is unavailable")
	}
	return a.respondClientWithFallback(nil, response)
}
