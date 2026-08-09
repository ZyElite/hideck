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

func buildIMSConfig(input imsConfigInput) (*imscore.IMSConfig, error) {
	prepared := input.session.Prepared
	imsPlan := prepared.CarrierPlan.IMS
	identity := prepared.IMSIdentity
	localIP := net.ParseIP(input.result.LocalAddr)
	value := imscore.BuildIMSConfigFromCarrier(
		input.session.DeviceID, prepared.Profile.IMSI, prepared.Profile.IMEI,
		prepared.Profile.MCC, prepared.Profile.MNC, prepared.Profile.IMSDomain,
		prepared.Profile.UserAgent, input.result.LocalAddr, prepared.CarrierPlan,
	)
	if err := imscore.ApplyResolvedIMSIdentityToConfig(&value, identity, prepared.Profile.MCC); err != nil {
		return nil, fmt.Errorf("runtimecore: apply IMS identity: %w", err)
	}
	value.SMSC = prepared.Profile.SMSC
	value.EPDGAddr = prepared.EPDGAddr
	value.LocalIP = localIP
	value.Registrar = firstNonEmpty(
		assignedPCSCF(input.result.Snapshot, localIP), imsPlan.PCSCF, imsPlan.Registrar, value.Domain,
	)
	value.KeepaliveInterval = time.Duration(imsPlan.OptionsPingIntervalSeconds) * time.Second
	value.AKAProvider, value.IMSNetwork = input.aka, input.network
	value.DeliveryStore = adaptDeliveryStore(input.session.DeliveryStore)
	value.EventBus, value.TraceID = input.eventBus, input.session.TraceID
	value.PAccessNetworkCountry = imscore.CountryISO2FromMCC(prepared.Profile.MCC)
	value.RegisterTemplate = convertRegisterTemplate(imsPlan.RegisterTemplate, imsPlan.Transport)
	return &value, nil
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
