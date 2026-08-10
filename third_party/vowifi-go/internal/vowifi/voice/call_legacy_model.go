package voice

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
)

var (
	telPhonePattern    = regexp.MustCompile(`tel:(\+?\d+)`)
	sipPhonePattern    = regexp.MustCompile(`sip:(\+?\d+)@`)
	sipIdentityPattern = regexp.MustCompile(`<sip:([^@>]+)@`)
)

func legacyRequestIdentity(request *sip.Request) (string, string, string) {
	if request == nil {
		return "", "", ""
	}
	callID := ""
	if header := request.CallID(); header != nil {
		callID = strings.TrimSpace(header.Value())
	}
	return callID, sipAddressUser(request.From()), sipAddressUser(request.To())
}

func sipAddressUser(header sip.Header) string {
	switch typed := header.(type) {
	case *sip.FromHeader:
		return strings.TrimSpace(typed.Address.User)
	case *sip.ToHeader:
		return strings.TrimSpace(typed.Address.User)
	default:
		return ""
	}
}

func (c *Call) parseInviteRequest(request *sip.Request) {
	if c == nil || request == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if from := request.From(); from != nil {
		c.DialogState.CallerID = extractPhoneNumber(from.Value())
		c.DialogState.FromTag = sipHeaderTag(from)
	}
	if to := request.To(); to != nil {
		c.DialogState.CalleeID = extractPhoneNumber(to.Value())
		c.DialogState.ToTag = sipHeaderTag(to)
	}
	if callID := request.CallID(); callID != nil {
		c.DialogState.CallID = strings.TrimSpace(callID.Value())
		if c.DialogState.IMSCallID == "" {
			c.DialogState.IMSCallID = c.DialogState.CallID
		}
	}
	c.DialogState.RouteSet = parsedInviteRouteSet(request)
	c.Timers.SessionExpires = parsedInviteSessionExpires(request)
	c.MediaState.IMSSDP = append([]byte(nil), request.Body()...)
}

func (c *Call) parseLegacyClientInvite(request *sip.Request) {
	if c == nil || request == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.DialogState.OriginalRequest = request
	c.MediaState.ClientSDP = append([]byte(nil), request.Body()...)
	c.DialogState.ClientFromTag = sipHeaderTag(request.From())
	if callID := request.CallID(); callID != nil {
		c.DialogState.ClientCallID = callID.Value()
		c.clientCallID = callID.Value()
	}
}

func (c *Call) originalTraceID() string {
	if c == nil {
		return ""
	}
	if traceID := strings.TrimSpace(c.DialogState.IMSCallID); traceID != "" {
		return traceID
	}
	if traceID := strings.TrimSpace(c.DialogState.CallID); traceID != "" {
		return traceID
	}
	return common.NewTraceID()
}

func parsedInviteRouteSet(request *sip.Request) []string {
	var routes []string
	for _, header := range request.GetHeaders("Record-Route") {
		if value := strings.TrimSpace(header.Value()); value != "" {
			routes = append(routes, value)
		}
	}
	var flattened []string
	for _, route := range routes {
		for _, value := range strings.Split(route, ",") {
			flattened = append(flattened, strings.TrimSpace(value))
		}
	}
	for left, right := 0, len(flattened)-1; left < right; left, right = left+1, right-1 {
		flattened[left], flattened[right] = flattened[right], flattened[left]
	}
	return flattened
}

func parsedInviteSessionExpires(request *sip.Request) int {
	header := request.GetHeader("Session-Expires")
	if header == nil {
		return 0
	}
	value := strings.Split(header.Value(), ";")[0]
	expires, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return expires
}

func extractPhoneNumber(value string) string {
	for _, pattern := range []*regexp.Regexp{telPhonePattern, sipPhonePattern} {
		if matches := pattern.FindStringSubmatch(value); len(matches) > 1 {
			return matches[1]
		}
	}
	matches := sipIdentityPattern.FindStringSubmatch(value)
	if len(matches) < 2 {
		return ""
	}
	return strings.Split(matches[1], ";")[0]
}

func sipHeaderTag(header sip.Header) string {
	switch typed := header.(type) {
	case *sip.FromHeader:
		value, _ := typed.Params.Get("tag")
		return strings.TrimSpace(value)
	case *sip.ToHeader:
		value, _ := typed.Params.Get("tag")
		return strings.TrimSpace(value)
	default:
		return ""
	}
}
