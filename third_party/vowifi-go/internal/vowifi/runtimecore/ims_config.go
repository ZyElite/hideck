package runtimecore

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/netstack"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

const defaultIMSSIPPort = 5060

type imsConfigInput struct {
	session  SessionConfig
	result   *SessionResult
	aka      imscore.AKAProvider
	network  imscore.IMSNetwork
	eventBus *imscore.EventBus
}

func resolveIPSec3GPPInstaller(
	dataplaneMode string,
	userspaceNetwork *netstack.Network,
) imscore.IPSec3GPPInstaller {
	if userspaceNetwork != nil {
		return userspaceNetwork
	}
	if strings.ToLower(strings.TrimSpace(dataplaneMode)) == swu.DataplaneModeUserspace {
		return nil
	}
	return imscore.SystemIPSec3GPPInstaller{}
}

func buildIMSConfig(input imsConfigInput) *imscore.IMSConfig {
	prepared := input.session.Prepared
	imsPlan := prepared.CarrierPlan.IMS
	identity := prepared.IMSIdentity
	domain := firstNonEmpty(identity.Domain, imsPlan.Domain, prepared.Profile.IMSDomain)
	localIP := net.ParseIP(input.result.LocalAddr)
	return &imscore.IMSConfig{
		DeviceID: input.session.DeviceID, IMEI: prepared.Profile.IMEI, IMSI: prepared.Profile.IMSI,
		IMPI: identity.IMPI, IMPU: nonEmptyIdentities(identity.IMPU, identity.IMPI),
		Domain: domain, SMSC: prepared.Profile.SMSC, Realm: firstNonEmpty(imsPlan.Realm, domain),
		EPDGAddr: prepared.EPDGAddr, LocalIP: localIP, Transport: firstNonEmpty(imsPlan.Transport, "auto"),
		Registrar: firstNonEmpty(
			assignedPCSCF(input.result.Snapshot, localIP), imsPlan.PCSCF, imsPlan.Registrar, domain,
		),
		LocalPort:         imsPlan.LocalPort,
		KeepaliveInterval: time.Duration(imsPlan.OptionsPingIntervalSeconds) * time.Second,
		AKAProvider:       input.aka, IMSNetwork: input.network,
		DeliveryStore: adaptDeliveryStore(input.session.DeliveryStore),
		EventBus:      input.eventBus, IPSec3GPPEnabled: true, TraceID: input.session.TraceID,
		UserAgent:             firstNonEmpty(imsPlan.UserAgent, prepared.Profile.UserAgent),
		CellularNetworkInfo:   imscore.GenerateDefaultCellularNetworkInfo(prepared.Profile.MCC, prepared.Profile.MNC),
		PAccessNetworkCountry: imscore.CountryISO2FromMCC(prepared.Profile.MCC),
		RegisterTemplate:      convertRegisterTemplate(imsPlan.RegisterTemplate, imsPlan.Transport),
	}
}

func assignedPCSCF(snapshot swu.SessionSnapshot, localIP net.IP) string {
	servers := snapshot.PCSCFv6
	if localIP != nil && localIP.To4() != nil {
		servers = snapshot.PCSCFv4
	}
	if len(servers) == 0 || servers[0] == nil {
		return ""
	}
	return net.JoinHostPort(servers[0].String(), fmt.Sprint(defaultIMSSIPPort))
}

func convertRegisterTemplate(
	template policy.IMSRegisterTemplate,
	transport string,
) imscore.IMSRegisterTemplate {
	return imscore.IMSRegisterTemplate{
		Expires:         time.Duration(template.Expires) * time.Second,
		Transport:       firstNonEmpty(template.Transport, transport),
		SupportedHeader: template.SupportedHeader, AllowHeader: template.AllowHeader,
		ContactMode: template.ContactMode, AccessType: template.AccessType, ICSIRef: template.ICSIRef,
		ContactOrder:              append([]string(nil), template.ContactOrder...),
		IncludePANIAuthenticated:  template.IncludePANIAuthenticated,
		StrictSecurityServerOffer: template.StrictSecurityServerOffer,
	}
}

func nonEmptyIdentities(impu, impi string) []string {
	if value := strings.TrimSpace(impu); value != "" {
		return []string{value}
	}
	if value := strings.TrimSpace(impi); value != "" {
		return []string{"sip:" + value}
	}
	return nil
}
