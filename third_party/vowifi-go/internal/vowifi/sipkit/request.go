package sipkit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsheaders"
)

const defaultMaxForwards = 70

// BuildIMSRequest builds a complete IMS request from immutable runtime and
// transaction inputs.
func BuildIMSRequest(
	method sip.RequestMethod,
	recipient sip.Uri,
	options IMSRequestOptions,
) (*sip.Request, error) {
	if method == "" {
		return nil, errors.New("empty method")
	}
	request := sip.NewRequest(method, *recipient.Clone())
	if destination := strings.TrimSpace(options.Destination); destination != "" {
		request.SetDestination(destination)
	}
	transport := pickTransport(options.Transport, options.Runtime.Transport)
	via, err := buildViaHeader(transport, options)
	if err != nil {
		return nil, err
	}
	request.AppendHeader(via)
	if err := appendRouteSet(request, options); err != nil {
		return nil, err
	}
	if err := appendTransactionHeaders(request, method, options); err != nil {
		return nil, err
	}
	appendIMSHeaders(request, method, options)
	if options.AddUserAgent {
		userAgent := pickUserAgent(options.UserAgent, options.Runtime.UserAgent)
		if userAgent == "" {
			return nil, fmt.Errorf("missing User-Agent for %s", options.Kind)
		}
		request.AppendHeader(sip.NewHeader("User-Agent", userAgent))
	}
	appendCustomHeaders(request, options.Headers)
	appendHeaderWhen(request, "Content-Type", options.ContentType, strings.TrimSpace(options.ContentType) != "")
	request.SetBody(options.Body)
	applyRequestTransport(request, transport, options.OmitURITransport)
	return request, nil
}

func buildViaHeader(transport string, options IMSRequestOptions) (*sip.ViaHeader, error) {
	host := strings.TrimSpace(options.ViaHost)
	port := options.ViaPort
	if host == "" || port < 1 {
		localHost, localPort := parseHostPortFromLocalAddr(options.Runtime.LocalAddr)
		if host == "" {
			host = localHost
		}
		if port < 1 {
			port = localPort
		}
	}
	if host == "" {
		return nil, errors.New("missing via host")
	}
	params := sip.NewParams()
	if options.AddRPort {
		params.Add("rport", "")
	}
	if branch := strings.TrimSpace(options.Branch); branch != "" {
		params.Add("branch", branch)
	}
	if options.AddAlias && transport == transportTCP {
		params.Add("alias", "")
	}
	return &sip.ViaHeader{
		ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: transport,
		Host: strings.Trim(normalizeHostForVia(host), "[]"), Port: port, Params: params,
	}, nil
}

func appendRouteSet(request *sip.Request, options IMSRequestOptions) error {
	routes := chooseRouteSet(options.Routes, options.Runtime.ServiceRoute)
	if options.RequireRoute && len(routes) == 0 {
		return fmt.Errorf("route is required for %s request", options.Kind)
	}
	for _, route := range routes {
		if route = strings.TrimSpace(route); route != "" {
			request.AppendHeader(sip.NewHeader("Route", route))
		}
	}
	return nil
}

func chooseRouteSet(explicit []string, serviceRoute string) []string {
	if len(explicit) > 0 {
		return explicit
	}
	return imsheaders.RouteSet(serviceRoute, "")
}

func appendTransactionHeaders(request *sip.Request, method sip.RequestMethod, options IMSRequestOptions) error {
	callID := strings.TrimSpace(options.CallID)
	if callID == "" {
		return errors.New("missing call-id")
	}
	if options.CSeq == 0 {
		return errors.New("missing cseq")
	}
	from := &sip.FromHeader{Address: *options.FromURI.Clone(), Params: tagParams(options.FromTag)}
	to := &sip.ToHeader{Address: *options.ToURI.Clone(), Params: tagParams(options.ToTag)}
	request.AppendHeader(from)
	request.AppendHeader(to)
	callIDHeader := sip.CallIDHeader(callID)
	request.AppendHeader(&callIDHeader)
	request.AppendHeader(&sip.CSeqHeader{SeqNo: options.CSeq, MethodName: method})
	maxForwards := options.MaxForwards
	if maxForwards <= 0 {
		maxForwards = defaultMaxForwards
	}
	maxForwardsHeader := sip.MaxForwardsHeader(maxForwards)
	request.AppendHeader(&maxForwardsHeader)
	if options.Contact != nil {
		request.AppendHeader(options.Contact)
	}
	return nil
}

func tagParams(tag string) sip.HeaderParams {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil
	}
	params := sip.NewParams()
	params.Add("tag", tag)
	return params
}

func appendIMSHeaders(request *sip.Request, method sip.RequestMethod, options IMSRequestOptions) {
	if !strings.EqualFold(strings.TrimSpace(options.SecurityMode), "disabled") {
		request.AppendHeader(sip.NewHeader("Require", "sec-agree"))
		request.AppendHeader(sip.NewHeader("Proxy-Require", "sec-agree"))
	}
	appendHeaderWhen(request, "Supported", options.Supported, options.AddSupported)
	appendHeaderWhen(request, "Allow", options.Allow, options.AddAllow)
	appendHeaderWhen(request, "P-Preferred-Service", options.PreferredService, options.AddPreferredService)
	appendHeaderWhen(request, "Accept-Contact", options.AcceptContact, options.AddAcceptContact)
	pani := pickPANI(options.PAccessNetworkInfo, options.Runtime.PAccessNetworkInfo)
	appendHeaderWhen(request, "P-Access-Network-Info", pani, requiresPANI(method, pani))
	appendHeaderWhen(request, "P-Preferred-Identity", options.PreferredIdentity,
		requiresPPI(method, options.PreferredIdentity))
	appendHeaderWhen(request, "Security-Client", options.SecurityClient,
		requiresSecurityClient(method, options.SecurityClient))
	securityVerify := pickSecurityVerify(options.SecurityVerify, options.Runtime.SecVerify)
	appendHeaderWhen(request, "Security-Verify", securityVerify,
		requiresSecurityVerify(method, securityVerify))
}

func appendCustomHeaders(request *sip.Request, headers []sip.Header) {
	for _, header := range headers {
		if header != nil {
			request.AppendHeader(header)
		}
	}
}

func pickTransport(explicit, runtimeValue string) string {
	if strings.TrimSpace(explicit) != "" {
		return normalizeSIPTransport(explicit)
	}
	return normalizeSIPTransport(runtimeValue)
}

func pickSecurityVerify(explicit, runtimeValue string) string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit
	}
	return strings.TrimSpace(runtimeValue)
}

func pickPANI(explicit, runtimeValue string) string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit
	}
	return strings.TrimSpace(runtimeValue)
}

func pickUserAgent(explicit, runtimeValue string) string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit
	}
	return strings.TrimSpace(runtimeValue)
}

func requiresHeader(include bool, value string) bool {
	return include && strings.TrimSpace(value) != ""
}
