package runtimecore

import (
	"context"
	"errors"
	"sync"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
	"github.com/iniwex5/vowifi-go/internal/vowifi/simauth"
	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
)

// RuntimeConfig is the additive configuration used by pre-restoration callers.
type RuntimeConfig struct {
	DeviceID     string
	Profile      profile.Profile
	EPDGOverride string
	IMSIdentity  profile.IMSIdentity
	AKAApp       profile.AKAAppPreference
	NetworkMode  string
	Access       Access
}

type Access interface {
	IMSIdentityProvider() IdentityProvider
	AKAProvider() AKAProvider
	Capabilities() AccessCapabilities
}

type IdentityProvider interface {
	GetISIMIdentity() (profile.IMSIdentity, error)
}

type AKAProvider = enginesim.AKAProvider
type AKAResult = enginesim.AKAResult

type AccessCapabilities struct {
	HasISIM bool
	HasUSIM bool
}

type PreparedSessionStart struct {
	Profile            profile.Profile
	IMSIdentity        profile.IMSIdentity
	IMSIdentityResult  profile.IMSIdentityResult
	AuthPlan           profile.AuthPlan
	EPDGAddr           string
	EPDGSource         string
	APN                string
	Carrier            carrier.EffectiveCarrierConfig
	IdentityIMEISource string
}

func PrepareCompatibilitySessionStart(cfg RuntimeConfig) (*PreparedSessionStart, error) {
	request, err := compatibilityRuntimeRequest(cfg)
	if err != nil {
		return nil, err
	}
	prepared, err := PrepareSessionStart(context.Background(), request)
	if err != nil {
		return nil, err
	}
	return compatibilityPreparedSession(prepared), nil
}

func BuildCompatibilitySWUConfig(
	prepared *PreparedSessionStart,
	aka AKAProvider,
) (*swu.Config, error) {
	if prepared == nil {
		return nil, errors.New("runtimecore: nil prepared session")
	}
	if aka == nil {
		return nil, errors.New("runtimecore: no SWu AKA provider")
	}
	converted := preparedFromCompatibility(prepared)
	config := BuildSWUConfig(SessionConfig{
		Prepared: converted,
		SIM:      compatibilitySIMAdapter{aka: aka},
	})
	if err := swu.ValidateProposalConfig(config); err != nil {
		return nil, err
	}
	return config, nil
}

// AdaptCompatibilityPreparedSession converts the additive host representation
// into the original runtimecore session model.
func AdaptCompatibilityPreparedSession(
	prepared *PreparedSessionStart,
) (profile.PreparedSession, error) {
	if prepared == nil {
		return profile.PreparedSession{}, errors.New("runtimecore: nil prepared session")
	}
	return validatePrepared(preparedFromCompatibility(prepared))
}

type ConfiguredRuntime struct {
	cfg      RuntimeConfig
	mu       sync.Mutex
	result   RuntimeStartResult
	prepared *PreparedSessionStart
}

func NewRuntime(cfg RuntimeConfig) *ConfiguredRuntime {
	return &ConfiguredRuntime{cfg: cfg}
}

func (runtime *ConfiguredRuntime) Start(ctx context.Context) error {
	if runtime == nil {
		return errors.New("runtimecore: nil runtime")
	}
	request, err := compatibilityRuntimeRequest(runtime.cfg)
	if err != nil {
		return err
	}
	prepared, err := PrepareSessionStart(ctx, request)
	if err != nil {
		return err
	}
	request.Prepared = &prepared
	result, err := (Runtime{}).Start(ctx, request)
	if err != nil {
		return err
	}
	runtime.mu.Lock()
	runtime.result = result
	runtime.prepared = compatibilityPreparedSession(prepared)
	runtime.mu.Unlock()
	return nil
}

func (runtime *ConfiguredRuntime) Stop() {
	if runtime == nil {
		return
	}
	runtime.mu.Lock()
	result := runtime.result
	runtime.result = RuntimeStartResult{}
	runtime.mu.Unlock()
	defaultStopSession(context.Background(), result.Session)
}

func (runtime *ConfiguredRuntime) Prepared() *PreparedSessionStart {
	if runtime == nil {
		return nil
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.prepared == nil {
		return nil
	}
	copy := *runtime.prepared
	return &copy
}

func compatibilityRuntimeRequest(cfg RuntimeConfig) (RuntimeStartRequest, error) {
	if cfg.Access == nil || cfg.Access.AKAProvider() == nil {
		return RuntimeStartRequest{}, errors.New("runtimecore: no SWu AKA provider")
	}
	identity := profile.IMSIdentityResult{
		AKAAppPreference: cfg.AKAApp, IMPI: cfg.IMSIdentity.IMPI,
		Domain: cfg.IMSIdentity.Domain, Applied: cfg.IMSIdentity.IMPI != "",
	}
	if len(cfg.IMSIdentity.IMPU) > 0 {
		identity.IMPU = cfg.IMSIdentity.IMPU[0]
	}
	return RuntimeStartRequest{
		DeviceID: cfg.DeviceID, Profile: cfg.Profile, RuntimeEPDGOverride: cfg.EPDGOverride,
		IMSIdentity: identity, SIM: compatibilitySIMAdapter{
			aka: cfg.Access.AKAProvider(), identity: cfg.Access.IMSIdentityProvider(),
		},
	}, nil
}

type compatibilitySIMAdapter struct {
	aka      AKAProvider
	identity IdentityProvider
}

func (adapter compatibilitySIMAdapter) EPDGSIMProvider(profile.AuthPlan) enginesim.AKAProvider {
	return adapter.aka
}

func (adapter compatibilitySIMAdapter) IMSAKAProvider(profile.AuthPlan) simauth.AKAProvider {
	return adapter.aka
}

func (adapter compatibilitySIMAdapter) IMSIdentityProvider() profile.Provider {
	if adapter.identity == nil {
		return nil
	}
	return compatibilityIdentityProvider{provider: adapter.identity}
}

type compatibilityIdentityProvider struct{ provider IdentityProvider }

func (provider compatibilityIdentityProvider) GetISIMIdentity() (profile.Identity, error) {
	return provider.provider.GetISIMIdentity()
}

func preparedFromCompatibility(prepared *PreparedSessionStart) profile.PreparedSession {
	identity := compatibilityIdentityResult(prepared)
	return profile.PreparedSession{
		Profile: prepared.Profile, IMSIdentity: identity, AuthPlan: prepared.AuthPlan,
		CarrierPlan: carrierPlanFromCompatibility(prepared.Carrier),
		EPDGAddr:    prepared.EPDGAddr, EPDGSource: prepared.EPDGSource,
		IdentityIMEISource: prepared.IdentityIMEISource,
	}
}

func compatibilityIdentityResult(prepared *PreparedSessionStart) profile.IMSIdentityResult {
	identity := prepared.IMSIdentityResult
	if identity.IMPI != "" || identity.IMPU != "" || identity.Domain != "" {
		return identity
	}
	identity.Applied = prepared.IMSIdentity.IMPI != ""
	identity.IMPI = prepared.IMSIdentity.IMPI
	identity.Domain = prepared.IMSIdentity.Domain
	if len(prepared.IMSIdentity.IMPU) > 0 {
		identity.IMPU = prepared.IMSIdentity.IMPU[0]
	}
	return identity
}

func compatibilityPreparedSession(prepared profile.PreparedSession) *PreparedSessionStart {
	return &PreparedSessionStart{
		Profile: prepared.Profile,
		IMSIdentity: profile.Identity{
			IMPI: prepared.IMSIdentity.IMPI, IMPU: nonEmptyStringSlice(prepared.IMSIdentity.IMPU),
			Domain: prepared.IMSIdentity.Domain,
		},
		IMSIdentityResult:  prepared.IMSIdentity,
		AuthPlan:           prepared.AuthPlan,
		EPDGAddr:           prepared.EPDGAddr,
		EPDGSource:         prepared.EPDGSource,
		APN:                prepared.CarrierPlan.EPDG.APN,
		IdentityIMEISource: prepared.IdentityIMEISource,
	}
}

func carrierPlanFromCompatibility(value carrier.EffectiveCarrierConfig) policy.CarrierPlan {
	effective := policy.EffectiveCarrierConfig{
		MCC: value.MCC, MNC: value.MNC, PresetID: value.PresetID,
		MatchedTemplate: value.MatchedTemplate, IPStackType: value.IPStackType,
		EPDGAddr: value.EPDGAddr, EPDGAddrSource: value.EPDGAddrSource,
		EPDGPort: value.EPDGPort, APN: value.APN, DNSServer: value.DNSServer,
		NATKeepaliveSeconds: value.NATKeepaliveSeconds, DPDIntervalSeconds: value.DPDIntervalSeconds,
		AKAChallengeMode: value.AKAChallengeMode, IKEIdentityMode: value.IKEIdentityMode,
		AKAIdentityMode: value.AKAIdentityMode, IKEProposals: append([]string(nil), value.IKEProposals...),
		ESPProposals: append([]string(nil), value.ESPProposals...), EnableLegacyCiphers: value.EnableLegacyCiphers,
		AllowedLegacyCiphers: append([]string(nil), value.AllowedLegacyCiphers...),
		AlgorithmPolicy:      value.AlgorithmPolicy, DeviceIdentityIMEI: value.DeviceIdentityIMEI,
		DeviceIdentityEnabled: value.DeviceIdentityEnabled, DeviceModel: value.DeviceModel,
		IMSDomain: value.IMSDomain, IMSRealm: value.IMSRealm, IMSRegistrar: value.IMSRegistrar,
		IMSPCSCF: value.IMSPCSCF, IMSUserAgent: value.IMSUserAgent, IMSTransport: value.IMSTransport,
		IMSIdentitySource: value.IMSIdentitySource, IMSLocalPort: value.IMSLocalPort,
		IMSTCPKeepaliveSeconds:        value.IMSTCPKeepaliveSeconds,
		IMSOptionsPingIntervalSeconds: value.IMSOptionsPingIntervalSeconds,
		DPDKeepaliveIntervalSeconds:   value.DPDKeepaliveIntervalSeconds,
		ReauthIntervalSeconds:         value.ReauthIntervalSeconds,
		SMSRoutingMethod:              value.SMSRoutingMethod, SMSRoutingGW: value.SMSRoutingGW,
		ForceSMSCAuth: value.ForceSMSCAuth,
	}
	return policy.CarrierPlanFromEffectiveConfig(effective)
}

func nonEmptyStringSlice(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}
