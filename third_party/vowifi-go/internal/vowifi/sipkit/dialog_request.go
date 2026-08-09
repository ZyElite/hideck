package sipkit

import (
	"errors"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsheaders"
)

// BuildMinimalDialogRequest creates the non-transaction headers for an
// in-dialog request. The caller supplies Via, Route, From, To, Call-ID, CSeq,
// and Max-Forwards through options.Headers.
func BuildMinimalDialogRequest(
	method sip.RequestMethod,
	recipient sip.Uri,
	options DialogRequestOptions,
) (*sip.Request, error) {
	if method == "" {
		return nil, errors.New("empty method")
	}
	request := sip.NewRequest(method, *recipient.Clone())
	for _, header := range options.Headers {
		if header != nil && !isDialogOwnedHeader(header.Name()) {
			request.AppendHeader(sip.HeaderClone(header))
		}
	}
	applyDialogPANI(request, method, options.PAccessNetworkInfo, options.ForcePANI)
	ApplyAutoHeaders(request, method, "", options.PreferredIdentity, "")
	applyDialogSecurityHeaders(request, method, options.SecurityVerify, options.Protected)
	appendHeaderWhen(request, "User-Agent", options.UserAgent, strings.TrimSpace(options.UserAgent) != "")
	appendHeaderWhen(request, "Content-Type", options.ContentType, strings.TrimSpace(options.ContentType) != "")
	request.SetBody(options.Body)
	return request, nil
}

func applyDialogPANI(request *sip.Request, method sip.RequestMethod, value string, force bool) {
	value = strings.TrimSpace(value)
	appendHeaderWhen(request, "P-Access-Network-Info", value, value != "" && (force || requiresPANI(method, value)))
}

func applyDialogSecurityHeaders(request *sip.Request, method sip.RequestMethod, value string, protected bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if !protected {
		appendHeaderWhen(request, "Security-Verify", value, requiresSecurityVerify(method, value))
		return
	}
	for _, header := range imsheaders.SecAgreeProtectedHeaders(value) {
		request.AppendHeader(sip.NewHeader(header.Name, header.Value))
	}
}

func isDialogOwnedHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "to", "via", "cseq", "from", "route", "call-id", "contact", "max-forwards", "content-length":
		return true
	default:
		return false
	}
}
