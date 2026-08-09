package policy

import "reflect"

func (plan CarrierPlan) IsZero() bool {
	return reflect.DeepEqual(plan, CarrierPlan{})
}

func CarrierPlanFromEffectiveConfig(config EffectiveCarrierConfig) CarrierPlan {
	return CarrierPlan{
		Metadata: CarrierMetadataPlan{
			MCC: config.MCC, MNC: config.MNC, PresetID: config.PresetID,
			MatchedTemplate: config.MatchedTemplate,
		},
		E911: E911Plan{
			Enabled: config.E911.Enabled, Provider: config.E911.Provider,
			EntitlementURL: config.E911.EntitlementURL, WebsheetHostPolicy: config.E911.WebsheetHostPolicy,
		},
		EPDG: EPDGPlan{
			IPStackType: config.IPStackType, Addr: config.EPDGAddr, AddrSource: config.EPDGAddrSource,
			Port: config.EPDGPort, APN: config.APN, DNSServer: config.DNSServer,
		},
		IKE: IKEPlan{
			NATKeepaliveSeconds: config.NATKeepaliveSeconds, DPDIntervalSeconds: config.DPDIntervalSeconds,
			AKAChallengeMode: config.AKAChallengeMode, IKEIdentityMode: config.IKEIdentityMode,
			AKAIdentityMode: config.AKAIdentityMode, IKEProposals: cloneStrings(config.IKEProposals),
			ESPProposals: cloneStrings(config.ESPProposals), EnableLegacyCiphers: config.EnableLegacyCiphers,
			AllowedLegacyCiphers: cloneStrings(config.AllowedLegacyCiphers), AlgorithmPolicy: config.AlgorithmPolicy,
			DPDKeepaliveIntervalSeconds: config.DPDKeepaliveIntervalSeconds,
			ReauthIntervalSeconds:       config.ReauthIntervalSeconds,
		},
		IMS: IMSPlan{
			Domain: config.IMSDomain, Realm: config.IMSRealm, Registrar: config.IMSRegistrar,
			PCSCF: config.IMSPCSCF, UserAgent: config.IMSUserAgent, Transport: config.IMSTransport,
			IdentitySource: config.IMSIdentitySource, LocalPort: config.IMSLocalPort,
			TCPKeepaliveSeconds:        config.IMSTCPKeepaliveSeconds,
			OptionsPingIntervalSeconds: config.IMSOptionsPingIntervalSeconds,
			RegisterTemplate:           cloneIMSRegisterTemplate(config.IMSRegisterTemplate),
			RegisterPolicySource:       config.IMSRegisterPolicySource,
		},
		SMS: SMSPlan{RoutingMethod: config.SMSRoutingMethod, RoutingGW: config.SMSRoutingGW, ForceSMSCAuth: config.ForceSMSCAuth},
		Device: DeviceIdentityPlan{
			IdentityIMEI: config.DeviceIdentityIMEI, IdentityEnabled: config.DeviceIdentityEnabled,
			Model: config.DeviceModel,
		},
	}
}

func EffectiveCarrierConfigFromCarrierPlan(plan CarrierPlan) EffectiveCarrierConfig {
	config := EffectiveCarrierConfig{
		MCC: plan.Metadata.MCC, MNC: plan.Metadata.MNC, PresetID: plan.Metadata.PresetID,
		MatchedTemplate: plan.Metadata.MatchedTemplate,
		E911: E911Policy{
			Enabled: plan.E911.Enabled, Provider: plan.E911.Provider,
			EntitlementURL: plan.E911.EntitlementURL, WebsheetHostPolicy: plan.E911.WebsheetHostPolicy,
		},
		IPStackType: plan.EPDG.IPStackType, EPDGAddr: plan.EPDG.Addr,
		EPDGAddrSource: plan.EPDG.AddrSource, EPDGPort: plan.EPDG.Port,
		APN: plan.EPDG.APN, DNSServer: plan.EPDG.DNSServer,
		NATKeepaliveSeconds: plan.IKE.NATKeepaliveSeconds, DPDIntervalSeconds: plan.IKE.DPDIntervalSeconds,
		AKAChallengeMode: plan.IKE.AKAChallengeMode, IKEIdentityMode: plan.IKE.IKEIdentityMode,
		AKAIdentityMode: plan.IKE.AKAIdentityMode, IKEProposals: cloneStrings(plan.IKE.IKEProposals),
		ESPProposals: cloneStrings(plan.IKE.ESPProposals), EnableLegacyCiphers: plan.IKE.EnableLegacyCiphers,
		AllowedLegacyCiphers: cloneStrings(plan.IKE.AllowedLegacyCiphers), AlgorithmPolicy: plan.IKE.AlgorithmPolicy,
		DPDKeepaliveIntervalSeconds: plan.IKE.DPDKeepaliveIntervalSeconds,
		ReauthIntervalSeconds:       plan.IKE.ReauthIntervalSeconds,
		IMSDomain:                   plan.IMS.Domain, IMSRealm: plan.IMS.Realm, IMSRegistrar: plan.IMS.Registrar,
		IMSPCSCF: plan.IMS.PCSCF, IMSUserAgent: plan.IMS.UserAgent, IMSTransport: plan.IMS.Transport,
		IMSIdentitySource: plan.IMS.IdentitySource, IMSLocalPort: plan.IMS.LocalPort,
		IMSTCPKeepaliveSeconds:        plan.IMS.TCPKeepaliveSeconds,
		IMSOptionsPingIntervalSeconds: plan.IMS.OptionsPingIntervalSeconds,
		IMSRegisterTemplate:           cloneIMSRegisterTemplate(plan.IMS.RegisterTemplate),
		IMSRegisterPolicySource:       plan.IMS.RegisterPolicySource,
		SMSRoutingMethod:              plan.SMS.RoutingMethod, SMSRoutingGW: plan.SMS.RoutingGW,
		ForceSMSCAuth: plan.SMS.ForceSMSCAuth, DeviceIdentityIMEI: plan.Device.IdentityIMEI,
		DeviceIdentityEnabled: plan.Device.IdentityEnabled, DeviceModel: plan.Device.Model,
	}
	syncCompatibilityProjection(&config)
	return config
}

func cloneIMSRegisterTemplate(template IMSRegisterTemplate) IMSRegisterTemplate {
	template.ContactParamOrder = cloneStrings(template.ContactParamOrder)
	template.SecurityClientMechanisms = cloneMechanisms(template.SecurityClientMechanisms)
	template.RegisterPolicy.TemporaryStatusCodes = cloneInts(template.RegisterPolicy.TemporaryStatusCodes)
	template.RegisterPolicy.ForbiddenStatusCodes = cloneInts(template.RegisterPolicy.ForbiddenStatusCodes)
	template.RegisterPolicy.InitialRejectFallbackStatusCodes = cloneInts(template.RegisterPolicy.InitialRejectFallbackStatusCodes)
	template.ContactOrder = cloneStrings(template.ContactOrder)
	return template
}
