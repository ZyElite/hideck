package ussi

import (
	"bytes"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
)

func buildInitialInvite(cfg Config, session *Session, command string) (string, error) {
	xmlBody, err := requestXML(command)
	if err != nil {
		return "", err
	}
	boundary := "vowifi-ussi-" + token(session.id)
	body := BuildMultipartBodyWithBoundary(cfg.LocalAddress, boundary, xmlBody)
	var request strings.Builder
	fmt.Fprintf(&request, "INVITE %s SIP/2.0\r\n", session.remoteURI)
	writeCommonHeaders(&request, cfg, session, "INVITE", session.inviteBranch)
	fmt.Fprintf(&request, "Contact: <%s>\r\n", cfg.ContactURI)
	request.WriteString("P-Preferred-Service: urn:urn-7:3gpp-service.ims.icsi.ussd\r\n")
	request.WriteString("Accept-Contact: *;+g.3gpp.ussd\r\n")
	request.WriteString("Recv-Info: " + InfoPackage + "\r\n")
	request.WriteString("Accept: " + ContentType + ", application/sdp, multipart/mixed\r\n")
	fmt.Fprintf(&request, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary)
	fmt.Fprintf(&request, "Content-Length: %d\r\n\r\n", len(body))
	request.Write(body)
	return request.String(), nil
}

func buildDialogRequest(cfg Config, session *Session, method string, body []byte) string {
	branch := "z9hG4bK" + randomHex(12)
	var request strings.Builder
	fmt.Fprintf(&request, "%s %s SIP/2.0\r\n", method, session.remoteTarget)
	writeCommonHeaders(&request, cfg, session, method, branch)
	if method == "INFO" {
		request.WriteString("Info-Package: " + InfoPackage + "\r\n")
		request.WriteString("Content-Disposition: " + ContentDisposition + "\r\n")
		request.WriteString("Recv-Info: " + InfoPackage + "\r\n")
		request.WriteString("Accept: " + ContentType + "\r\n")
		request.WriteString("Content-Type: " + ContentType + "\r\n")
	}
	fmt.Fprintf(&request, "Content-Length: %d\r\n\r\n", len(body))
	request.Write(body)
	return request.String()
}

func buildACK(cfg Config, session *Session, finalStatus int) string {
	branch := "z9hG4bK" + randomHex(12)
	if finalStatus >= 300 {
		branch = session.inviteBranch
	}
	var request strings.Builder
	fmt.Fprintf(&request, "ACK %s SIP/2.0\r\n", session.remoteTarget)
	writeCommonHeaders(&request, cfg, session, "ACK", branch)
	request.WriteString("Content-Length: 0\r\n\r\n")
	return request.String()
}

func writeCommonHeaders(out *strings.Builder, cfg Config, session *Session, method, branch string) {
	transport := normalizedTransport(cfg.SIPTransport)
	fmt.Fprintf(out, "Via: SIP/2.0/%s %s;rport;branch=%s\r\n", transport, cfg.LocalAddress, branch)
	for _, route := range session.routeSet {
		fmt.Fprintf(out, "Route: %s\r\n", route)
	}
	fmt.Fprintf(out, "From: <%s>;tag=%s\r\n", cfg.LocalURI, session.localTag)
	to := "<" + session.remoteURI + ">"
	if session.remoteTag != "" {
		to += ";tag=" + session.remoteTag
	}
	fmt.Fprintf(out, "To: %s\r\n", to)
	fmt.Fprintf(out, "Call-ID: %s\r\nCSeq: %d %s\r\n", session.callID, session.cseq, method)
	out.WriteString("Max-Forwards: 70\r\n")
	writeOptionalHeader(out, "P-Preferred-Identity", "<"+cfg.LocalURI+">")
	writeOptionalHeader(out, "Security-Verify", cfg.SecurityVerify)
	writeOptionalHeader(out, "P-Access-Network-Info", cfg.PANI)
	writeOptionalHeader(out, "User-Agent", cfg.UserAgent)
}

func writeOptionalHeader(out *strings.Builder, name, value string) {
	if strings.TrimSpace(value) != "" {
		fmt.Fprintf(out, "%s: %s\r\n", name, strings.TrimSpace(value))
	}
}

func learnDialog(session *Session, response Response) {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.remoteTag = headerTag(responseHeader(response.Headers, "To"))
	if contact := headerURI(responseHeader(response.Headers, "Contact")); contact != "" {
		session.remoteTarget = contact
	}
	routes := splitHeaderValues(responseHeader(response.Headers, "Record-Route"))
	for left, right := 0, len(routes)-1; left < right; left, right = left+1, right-1 {
		routes[left], routes[right] = routes[right], routes[left]
	}
	session.routeSet = routes
}

// BuildMultipartBody preserves the original helper signature.
func BuildMultipartBody(sdp, ussiXML []byte) []byte {
	return buildMultipart("----=_Part_0_1", sdp, ussiXML)
}

func BuildMultipartBodyWithBoundary(localAddress, boundary string, ussiXML []byte) []byte {
	sdp := BuildSDP(hostOnly(localAddress), "")
	return buildMultipart(boundary, sdp, ussiXML)
}

func buildMultipart(boundary string, sdp, ussiXML []byte) []byte {
	var out bytes.Buffer
	writer := multipart.NewWriter(&out)
	_ = writer.SetBoundary(boundary)
	sdpHeader := textproto.MIMEHeader{"Content-Type": {"application/sdp"}}
	sdpPart, _ := writer.CreatePart(sdpHeader)
	_, _ = sdpPart.Write(sdp)
	ussiHeader := textproto.MIMEHeader{
		"Content-Type":        {ContentType},
		"Content-Disposition": {ContentDisposition},
	}
	ussiPart, _ := writer.CreatePart(ussiHeader)
	_, _ = ussiPart.Write(ussiXML)
	_ = writer.Close()
	return out.Bytes()
}

// ExtractFromMultipart extracts one MIME part by media type.
func ExtractFromMultipart(body []byte, contentType string) []byte {
	mediaType, params, err := mime.ParseMediaType(contentType)
	targetType := ContentType
	boundary := ""
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		targetType = strings.TrimSpace(mediaType)
		boundary = multipartBoundary(body)
	} else {
		boundary = params["boundary"]
	}
	return extractMultipartPart(body, boundary, targetType)
}

func extractMultipartPart(body []byte, boundary, targetType string) []byte {
	if strings.TrimSpace(boundary) == "" {
		return nil
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, readErr := reader.NextPart()
		if readErr != nil {
			return nil
		}
		partBody := new(bytes.Buffer)
		_, _ = partBody.ReadFrom(part)
		partType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if strings.EqualFold(partType, targetType) {
			return partBody.Bytes()
		}
	}
}

func multipartBoundary(body []byte) string {
	firstLine, _, _ := bytes.Cut(body, []byte("\n"))
	line := strings.TrimSpace(string(firstLine))
	if strings.HasPrefix(line, "--") {
		return strings.TrimPrefix(line, "--")
	}
	return ""
}

func extractUSSI(contentType string, body []byte) ([]byte, bool, error) {
	if IsContentType(contentType) {
		return body, true, nil
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, false, err
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		return nil, false, nil
	}
	part := ExtractFromMultipart(body, contentType)
	return part, len(part) > 0, nil
}

// BuildSDP builds the message-media SDP carried in the initial INVITE.
func BuildSDP(localIP, _ string) []byte {
	if strings.TrimSpace(localIP) == "" {
		localIP = "0.0.0.0"
	}
	return []byte(fmt.Sprintf("v=0\r\no=- 0 0 IN IP4 %s\r\ns=-\r\nc=IN IP4 %s\r\nt=0 0\r\nm=message 0 TCP/MSRP *\r\na=recvonly\r\n", localIP, localIP))
}

func responseHeader(headers map[string]string, name string) string {
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func headerTag(value string) string {
	for _, param := range strings.Split(value, ";")[1:] {
		name, tag, ok := strings.Cut(strings.TrimSpace(param), "=")
		if ok && strings.EqualFold(name, "tag") {
			return strings.Trim(strings.TrimSpace(tag), "\"")
		}
	}
	return ""
}

func headerURI(value string) string {
	start, end := strings.IndexByte(value, '<'), strings.IndexByte(value, '>')
	if start >= 0 && end > start {
		return strings.TrimSpace(value[start+1 : end])
	}
	return strings.TrimSpace(strings.Split(value, ";")[0])
}

func splitHeaderValues(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func normalizedTransport(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "tcp") {
		return "TCP"
	}
	return "UDP"
}

func hostOnly(address string) string {
	host := strings.TrimSpace(address)
	if index := strings.LastIndexByte(host, ':'); index > 0 {
		return strings.Trim(host[:index], "[]")
	}
	return strings.Trim(host, "[]")
}
