package dialog

import (
	"net"
	"strconv"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsdialog"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsheaders"
)

var defaultVoiceContactOrder = []string{
	"access_type", "sip_instance", "audio", "smsip", "icsi_ref",
}

func (c *Controller) contextLocked() imsdialog.Context {
	if c == nil || c.endpoint == nil {
		return imsdialog.Context{}
	}
	snapshot := c.endpoint.Snapshot()
	c.refreshCachedHeaders(snapshot)
	return imsdialog.Context{
		IMPU: snapshot.IMPU, Realm: snapshot.Realm, ContactID: snapshot.ContactID,
		LocalAddr: snapshot.LocalAddr, LocalPortC: snapshot.LocalPortC, LocalPortS: snapshot.LocalPortS,
		Transport: snapshot.Transport, ServiceRoute: snapshot.ServiceRoute, SecVerify: snapshot.SecVerify,
		SecMode: snapshot.EffectiveSecMode, PAccessNetworkInfo: snapshot.PAccessNetworkInfo,
		UserAgent: snapshot.UserAgent, IMEI: snapshot.IMEI, PubGRUU: snapshot.PubGRUU,
		DeviceID: c.deviceID, CachedFromURI: c.cachedFromURI,
		CachedContactHdr: c.cachedContactHdr, CachedRouteHdr: c.cachedRouteHdr,
		CachedSecVerifyHdr:     genericHeader("Security-Verify", snapshot.SecVerify),
		CachedPANIHdr:          genericHeader("P-Access-Network-Info", snapshot.PAccessNetworkInfo),
		CachedPPIHdr:           genericHeader("P-Preferred-Identity", preferredIdentity(snapshot.IMPU)),
		VoiceSupportedHeader:   snapshot.Voice.SupportedHeader,
		VoiceAllowHeader:       snapshot.Voice.AllowHeader,
		VoiceAcceptContact:     snapshot.Voice.AcceptContact,
		VoicePPreferredService: snapshot.Voice.PPreferredService,
		VoiceAccessType:        snapshot.Voice.AccessType,
		VoiceContactParamOrder: append([]string(nil), snapshot.Voice.ContactParamOrder...),
	}
}

func (c *Controller) refreshCachedHeaders(snapshot imsendpoint.Snapshot) {
	sessionHash := dialogSessionHash(snapshot)
	if sessionHash == c.lastSessionHash {
		return
	}
	c.lastSessionHash = sessionHash
	c.cachedFromURI = parseIdentityURI(snapshot.IMPU)
	c.cachedContactHdr = buildContactHeader(snapshot)
	c.cachedRouteHdr = genericHeader("Route", snapshot.ServiceRoute)
}

func dialogSessionHash(snapshot imsendpoint.Snapshot) string {
	parts := []string{
		snapshot.IMPU, snapshot.Realm, snapshot.ContactID,
		strconv.Itoa(snapshot.LocalPortC), strconv.Itoa(snapshot.LocalPortS),
		snapshot.LocalAddr, snapshot.Transport, snapshot.ServiceRoute,
		snapshot.IMEI, snapshot.PubGRUU, snapshot.Voice.AccessType,
		strings.Join(snapshot.Voice.ContactParamOrder, ","),
	}
	return strings.Join(parts, "|")
}

func parseIdentityURI(value string) sip.Uri {
	value = strings.TrimSpace(value)
	if value == "" {
		return sip.Uri{}
	}
	if !strings.Contains(value, ":") {
		value = "sip:" + value
	}
	var uri sip.Uri
	_ = sip.ParseUri(value, &uri)
	return uri
}

func buildContactHeader(snapshot imsendpoint.Snapshot) *sip.ContactHeader {
	options := contactOptions(snapshot)
	contactURI := strings.Trim(strings.TrimSpace(snapshot.PubGRUU), "<>")
	if contactURI == "" {
		contactURI = imsheaders.ContactURIWithOptions(options, snapshot.Transport, true)
	}
	var uri sip.Uri
	_ = sip.ParseUri(contactURI, &uri)
	params := sip.NewParams()
	for _, param := range imsheaders.ContactParams(options) {
		params.Add(param.Name, param.Value)
	}
	return &sip.ContactHeader{Address: uri, Params: params}
}

func contactOptions(snapshot imsendpoint.Snapshot) imsheaders.ContactOptions {
	order := snapshot.Voice.ContactParamOrder
	if len(order) == 0 {
		order = defaultVoiceContactOrder
	}
	return imsheaders.ContactOptions{
		ContactID: snapshot.ContactID, LocalAddr: endpointLocalIP(snapshot.LocalAddr),
		LocalPortC: snapshot.LocalPortC, LocalPortS: snapshot.LocalPortS,
		AccessType: snapshot.Voice.AccessType, ContactParamOrder: order,
		SIPInstance: snapshot.IMEI, IMEI: snapshot.IMEI,
	}
}

func genericHeader(name, value string) sip.Header {
	if value = strings.TrimSpace(value); value != "" {
		return sip.NewHeader(name, value)
	}
	return nil
}

func preferredIdentity(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || (strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">")) {
		return value
	}
	return "<" + value + ">"
}

func endpointLocalIP(localAddr string) string {
	localAddr = strings.TrimSpace(localAddr)
	if host, _, err := net.SplitHostPort(localAddr); err == nil {
		return host
	}
	if strings.HasPrefix(localAddr, "[") && strings.HasSuffix(localAddr, "]") {
		return strings.Trim(localAddr, "[]")
	}
	if index := strings.LastIndex(localAddr, "]:"); index > 0 {
		return strings.Trim(localAddr[:index+1], "[]")
	}
	if strings.Count(localAddr, ":") == 1 {
		return strings.SplitN(localAddr, ":", 2)[0]
	}
	return strings.Trim(localAddr, "[]")
}
