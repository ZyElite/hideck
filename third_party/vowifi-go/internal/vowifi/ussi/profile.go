package ussi

import (
	"net"
	"strconv"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsheaders"
)

const (
	initialAccept = "application/sdp,application/3gpp-ims+xml," +
		"application/vnd.3gpp.ussd+xml,multipart/mixed"
	initialAllow         = "INVITE,ACK,CANCEL,BYE,UPDATE,PRACK,MESSAGE,REFER,NOTIFY,INFO,OPTIONS"
	initialSupported     = "100rel,replaces,from-change,histinfo,tdialog"
	initialService       = "urn:urn-7:3gpp-service.ims.icsi.mmtel"
	initialEarlyMedia    = "supported"
	initialAcceptContact = `*;+g.3gpp.icsi-ref="urn%3Aurn-7%3A3gpp-service.ims.icsi.mmtel";audio`
)

var initialContactParams = []string{
	`+g.3gpp.icsi-ref="urn%3Aurn-7%3A3gpp-service.ims.icsi.mmtel"`,
	"audio",
	"video",
	"+g.3gpp.nw-init-ussi",
	"+g.3gpp.mid-call",
	"+g.3gpp.srvcc-alerting",
}

// Profile contains the recovered USSI INVITE header policy.
type Profile struct {
	Accept            string
	Allow             string
	Supported         string
	PreferredService  string
	EarlyMedia        string
	AcceptContact     string
	Protected         bool
	ContactParams     []string
	PreferredIdentity string
	SecurityVerify    string
}

// ApplyInitialInvite appends the USSI feature and protected IMS headers.
func (p Profile) ApplyInitialInvite(request *sip.Request) {
	if request == nil {
		return
	}
	if p.Protected {
		for _, header := range imsheaders.SecAgreeProtectedHeaders(p.SecurityVerify) {
			request.AppendHeader(sip.NewHeader(header.Name, header.Value))
		}
	} else {
		appendHeader(request, "Security-Verify", p.SecurityVerify)
	}
	appendHeader(request, "P-Preferred-Identity", taggedAddress(p.PreferredIdentity, ""))
	appendHeader(request, "Allow", p.Allow)
	appendHeader(request, "Accept", p.Accept)
	request.AppendHeader(sip.NewHeader("Recv-Info", InfoPackage))
	appendHeader(request, "P-Preferred-Service", p.PreferredService)
	appendHeader(request, "Accept-Contact", p.AcceptContact)
	appendHeader(request, "Supported", p.Supported)
	appendHeader(request, "P-Early-Media", p.EarlyMedia)
}

// ContactHeaderParams parses ordered Contact parameters and de-duplicates keys.
func (p Profile) ContactHeaderParams() sip.HeaderParams {
	params := sip.NewParams()
	for _, raw := range p.ContactParams {
		name, value, _ := strings.Cut(strings.TrimSpace(raw), "=")
		name = strings.TrimSpace(name)
		if name != "" {
			params.Add(name, strings.TrimSpace(value))
		}
	}
	return params
}

func initialInviteProfile(ctx Context) Profile {
	return Profile{
		Accept: initialAccept, Allow: initialAllow, Supported: initialSupported,
		PreferredService: initialService, EarlyMedia: initialEarlyMedia,
		AcceptContact:     initialAcceptContact,
		Protected:         !strings.EqualFold(strings.TrimSpace(ctx.Mode), "disabled"),
		ContactParams:     append([]string(nil), initialContactParams...),
		PreferredIdentity: ctx.AOR, SecurityVerify: ctx.SecVerify,
	}
}

func appendHeader(request *sip.Request, name, value string) {
	if value = strings.TrimSpace(value); value != "" {
		request.AppendHeader(sip.NewHeader(name, value))
	}
}

func taggedAddress(address, tag string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if !strings.HasPrefix(address, "<") {
		address = "<" + address + ">"
	}
	if tag = strings.TrimSpace(tag); tag != "" {
		address += ";tag=" + tag
	}
	return address
}

func splitLocalAddr(address string, fallbackPort int) (string, int) {
	address = strings.TrimSpace(address)
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return strings.Trim(address, "[]"), fallbackPort
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil || parsedPort < 1 {
		return strings.Trim(host, "[]"), fallbackPort
	}
	return strings.Trim(host, "[]"), parsedPort
}
