package ussi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
)

const multipartBoundary = "vohive_ussd"

// BuildInitialInvite restores the v1.5.5 structured INVITE builder.
func BuildInitialInvite(
	ctx Context,
	command, callID, localTag, branch string,
	cseq uint32,
	ussiXML []byte,
) (*sip.Request, error) {
	domain := firstNonEmpty(ctx.Domain, ctx.Realm)
	if domain == "" {
		return nil, errors.New("USSI domain 为空")
	}
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("USSD command 为空")
	}
	if strings.TrimSpace(callID) == "" {
		return nil, errors.New("USSI Call-ID 为空")
	}
	if cseq == 0 {
		return nil, errors.New("USSI CSeq 为空")
	}
	recipient, err := dialstringURI(command, domain)
	if err != nil {
		return nil, err
	}
	from, err := parseURI(ctx.AOR)
	if err != nil {
		return nil, fmt.Errorf("USSI AOR: %w", err)
	}
	profile := initialInviteProfile(ctx)
	body := BuildMultipartBody(ctx.LocalIP, ussiXML)
	request, err := sipkit.BuildIMSRequest(sip.INVITE, recipient, inviteOptions(
		ctx, profile, from, recipient, callID, localTag, branch, cseq, body,
	))
	if err != nil {
		return nil, err
	}
	profile.ApplyInitialInvite(request)
	return request, nil
}

func inviteOptions(
	ctx Context,
	profile Profile,
	from, recipient sip.Uri,
	callID, localTag, branch string,
	cseq uint32,
	body []byte,
) sipkit.IMSRequestOptions {
	return sipkit.IMSRequestOptions{
		Destination: ctx.Destination, Transport: ctx.Transport,
		ViaHost: ctx.LocalIP, ViaPort: ctx.LocalPortC, Branch: branch,
		FromURI: from, FromTag: localTag, ToURI: recipient,
		CallID: callID, CSeq: cseq, Routes: contextRoutes(ctx),
		Contact: contactHeader(ctx, profile.ContactHeaderParams()), Body: body,
		Runtime: sipkit.IMSRuntimeSnapshot{
			Transport: ctx.Transport, LocalAddr: ctx.LocalIP,
			LocalPortC: ctx.LocalPortC, LocalPortS: ctx.LocalPortS,
			PAccessNetworkInfo: ctx.PANI, UserAgent: ctx.UserAgent,
		},
		Kind: sipkit.RequestKindOutOfDialog, SecurityMode: "disabled",
		AddRPort: true, AddAlias: normalizedTransport(ctx.Transport) == "TCP",
		AddUserAgent: strings.TrimSpace(ctx.UserAgent) != "",
		ContentType:  "multipart/mixed;boundary=" + multipartBoundary,
	}
}

// BuildInfo builds one in-dialog USSI INFO request.
func BuildInfo(session *Session, body []byte, ctx Context) (*sip.Request, error) {
	if len(body) == 0 {
		return nil, errors.New("USSI INFO body 为空")
	}
	return buildDialogRequest(session, sip.INFO, body, ctx)
}

func buildDialogRequest(
	session *Session,
	method sip.RequestMethod,
	body []byte,
	ctx Context,
) (*sip.Request, error) {
	if session == nil {
		return nil, errors.New("USSI session 为空")
	}
	recipient, err := dialogRequestURI(session, ctx)
	if err != nil {
		return nil, fmt.Errorf("USSI dialog remote URI: %w", err)
	}
	headers := dialogHeaders(method)
	return sipkit.BuildMinimalDialogRequest(method, recipient, sipkit.DialogRequestOptions{
		PAccessNetworkInfo: ctx.PANI, PreferredIdentity: ctx.AOR,
		SecurityVerify: ctx.SecVerify, Protected: !strings.EqualFold(ctx.Mode, "disabled"),
		UserAgent: ctx.UserAgent, ContentType: dialogContentType(method),
		Body: body, Headers: headers,
	})
}

func dialogRequestURI(session *Session, ctx Context) (sip.Uri, error) {
	if session == nil {
		return sip.Uri{}, errors.New("USSI session 为空")
	}
	session.mu.Lock()
	target := firstNonEmpty(session.RemoteTarget, session.RemoteURI)
	session.mu.Unlock()
	if target == "" {
		domain := firstNonEmpty(ctx.Domain, ctx.Realm)
		if domain == "" {
			return sip.Uri{}, errors.New("USSI dialog remote URI 为空")
		}
		target = "sip:" + domain
	}
	lower := strings.ToLower(target)
	if !strings.HasPrefix(lower, "sip:") && !strings.HasPrefix(lower, "tel:") {
		target = "sip:" + target
	}
	return parseURI(target)
}

func dialogHeaders(method sip.RequestMethod) []sip.Header {
	if method != sip.INFO {
		return nil
	}
	return []sip.Header{
		sip.NewHeader("Info-Package", InfoPackage),
		sip.NewHeader("Content-Disposition", ContentDisposition),
		sip.NewHeader("Recv-Info", InfoPackage),
		sip.NewHeader("Accept", ContentType),
	}
}

func dialogContentType(method sip.RequestMethod) string {
	if method == sip.INFO {
		return ContentType
	}
	return ""
}

// BuildMultipartBody builds the fixed v1.5.5 vohive_ussd MIME envelope.
func BuildMultipartBody(localIP string, ussiXML []byte) []byte {
	var body bytes.Buffer
	fmt.Fprintf(&body, "--%s\r\nContent-Type: application/sdp\r\n\r\n", multipartBoundary)
	body.Write(BuildSDP(localIP))
	fmt.Fprintf(&body, "\r\n--%s\r\n", multipartBoundary)
	body.WriteString("Content-Type: " + ContentType + "\r\n")
	body.WriteString("Content-Disposition: render;handling=optional\r\n\r\n")
	body.Write(ussiXML)
	fmt.Fprintf(&body, "\r\n--%s--\r\n", multipartBoundary)
	return body.Bytes()
}

// ExtractFromMultipart returns the USSI XML part from a fixed or declared boundary.
func ExtractFromMultipart(body []byte) []byte {
	boundary := multipartBoundary
	if first, _, found := bytes.Cut(body, []byte("\n")); found {
		line := strings.TrimSpace(string(first))
		if strings.HasPrefix(line, "--") {
			boundary = strings.TrimPrefix(line, "--")
		}
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err != nil {
			return nil
		}
		partBody, err := io.ReadAll(part)
		if err != nil {
			return nil
		}
		mediaType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if IsContentType(mediaType) {
			return bytes.TrimSpace(partBody)
		}
	}
}

// BuildSDP builds the exact v1.5.5 audio capability body used by USSI INVITE.
func BuildSDP(localIP string) []byte {
	address := strings.Trim(strings.TrimSpace(localIP), "[]")
	if address == "" {
		address = "0.0.0.0"
	}
	family := "IP4"
	if net.ParseIP(address) != nil && strings.Contains(address, ":") {
		family = "IP6"
	}
	sessionID := fmt.Sprint(time.Now().UnixMilli())
	prefix := fmt.Sprintf("v=0\r\no=- %s %s IN %s %s\r\ns=-\r\nc=IN %s %s\r\nt=0 0\r\n",
		sessionID, sessionID, family, address, family, address)
	return []byte(prefix + audioCapabilities)
}

const audioCapabilities = "m=audio 0 RTP/AVP 106 112 104 110 102 108 96 97\r\n" +
	"b=AS:153\r\nb=RS:600\r\nb=RR:2000\r\n" +
	"a=rtpmap:106 EVS/16000/1\r\na=fmtp:106 br=5.9-24.4;bw=nb-wb;ch-aw-recv=-1;max-red=0\r\n" +
	"a=rtpmap:112 EVS/16000/1\r\na=fmtp:112 br=9.6-128;bw=swb;ch-aw-recv=-1;max-red=0\r\n" +
	"a=rtpmap:104 AMR-WB/16000/1\r\na=fmtp:104 mode-change-capability=2;max-red=0\r\n" +
	"a=rtpmap:110 AMR-WB/16000/1\r\na=fmtp:110 octet-align=1;mode-change-capability=2;max-red=0\r\n" +
	"a=rtpmap:102 AMR/8000/1\r\na=fmtp:102 mode-change-capability=2;max-red=0\r\n" +
	"a=rtpmap:108 AMR/8000/1\r\na=fmtp:108 octet-align=1;mode-change-capability=2;max-red=0\r\n" +
	"a=rtpmap:96 telephone-event/16000\r\na=fmtp:96 0-15\r\n" +
	"a=rtpmap:97 telephone-event/8000\r\na=fmtp:97 0-15\r\n" +
	"a=sendrecv\r\na=maxptime:240\r\na=ptime:20\r\n"

func parseURI(value string) (sip.Uri, error) {
	var uri sip.Uri
	if err := sip.ParseUri(strings.TrimSpace(value), &uri); err != nil {
		return sip.Uri{}, err
	}
	return uri, nil
}

func dialstringURI(command, domain string) (sip.Uri, error) {
	command = strings.ReplaceAll(strings.TrimSpace(command), "#", "%23")
	return parseURI(fmt.Sprintf("sip:%s;phone-context=%s@%s;user=dialstring", command, domain, domain))
}

func contactHeader(ctx Context, params sip.HeaderParams) *sip.ContactHeader {
	port := ctx.LocalPortS
	if port < 1 {
		port = ctx.LocalPortC
	}
	uriParams := sip.NewParams()
	uriParams.Add("transport", strings.ToLower(normalizedTransport(ctx.Transport)))
	return &sip.ContactHeader{Address: sip.Uri{
		Scheme: "sip", User: ctx.ContactID, Host: strings.Trim(ctx.LocalIP, "[]"),
		Port: port, UriParams: uriParams,
	}, Params: params}
}

func contextRoutes(ctx Context) []string {
	route := firstNonEmpty(ctx.RouteHeader, ctx.ServiceRoute)
	if route == "" {
		return nil
	}
	return []string{route}
}

func normalizedTransport(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "UDP":
		return "UDP"
	case "TCP":
		return "TCP"
	default:
		return "TCP"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
