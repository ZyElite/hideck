package imscore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsheaders"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
)

const (
	registrationSubscriptionTimeout = 10 * time.Second
	registrationSubscriptionFlow    = "subscribe_reg"
)

func (s *Service) startRegistrationSubscription() {
	eligible, skipReason := s.registrationSubscriptionGate()
	if !eligible {
		logging.Info("IMS SUBSCRIBE(reg) skipped",
			"device", s.DeviceID(), "reason", skipReason)
		return
	}
	logging.Info("IMS SUBSCRIBE(reg) starting", "device", s.DeviceID())
	s.networkDone.Add(1)
	go func() {
		defer s.networkDone.Done()
		ctx, cancel := context.WithTimeout(context.Background(), registrationSubscriptionTimeout)
		defer cancel()
		if err := s.sendSubscribeReg(ctx); err != nil {
			s.reportSubscriptionRuntimeError(err)
		}
	}()
}

func (s *Service) hasProtectedRegistrationTransport() bool {
	eligible, _ := s.registrationSubscriptionGate()
	return eligible
}

func (s *Service) registrationSubscriptionGate() (bool, string) {
	if s == nil {
		return false, "service_nil"
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subscriptionGateLocked()
}

func (s *Service) subscriptionGateLocked() (bool, string) {
	if s.regState != regRegistered {
		return false, "not_registered:" + strings.TrimSpace(s.regState)
	}
	if s.registrationTCP == nil {
		return false, "no_registration_tcp"
	}
	if s.regSession == nil {
		return false, "no_reg_session"
	}
	if s.regSession.security == nil || strings.TrimSpace(s.regSession.security.verifyHeader) == "" {
		return false, "no_sec_agree"
	}
	return true, ""
}

func (s *Service) subscriptionEligibleLocked() bool {
	eligible, _ := s.subscriptionGateLocked()
	return eligible
}

func (s *Service) stopped() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

func (s *Service) reportRegistrationRuntimeError(err error) {
	if err == nil || s == nil || s.stopped() {
		return
	}
	logging.Info("IMS runtime reconnect requested", "device", s.DeviceID(), "err", err)
	select {
	case s.registerErrors <- err:
	default:
		logging.WarnRate("ims-runtime-error-overflow-"+s.DeviceID(), time.Minute,
			"IMS runtime error channel is full", "device", s.DeviceID(), "err", err)
	}
}

func (s *Service) reportSubscriptionRuntimeError(err error) {
	if err == nil || s.stopped() {
		return
	}
	if !s.hasProtectedRegistrationTransport() {
		logging.Debug("IMS SUBSCRIBE result discarded after registration changed",
			"device", s.DeviceID(), "err", err)
		return
	}
	// SUBSCRIBE(reg) is for network-initiated deregister NOTIFY. A reject or
	// timeout must not tear down a REGISTER that already succeeded — that
	// produced a one-second IMS-ready flash then a full SWu rebuild.
	logging.WarnRate("ims-subscribe-reg-"+s.DeviceID(), 30*time.Second,
		"IMS SUBSCRIBE(reg) failed; keeping current registration",
		"device", s.DeviceID(), "err", subscriptionRuntimeError(err))
}

func (s *Service) sendSubscribeReg(ctx context.Context) error {
	if !s.subscriptionInFlight.CompareAndSwap(false, true) {
		return nil
	}
	defer s.subscriptionInFlight.Store(false)
	s.subscribeMu.Lock()
	defer s.subscribeMu.Unlock()

	request, requestedExpires, err := s.buildRegistrationSubscription()
	if err != nil {
		return s.recordSubscriptionResult(nil, 0, err)
	}
	s.recordSubscriptionAttempt(time.Now(), requestedExpires)
	logging.Debug("IMS SUBSCRIBE(reg) outbound", "device", s.DeviceID(), "sip", logging.RedactSIPRaw(request.String()))
	response, _, err := s.dispatchOutboundRequest(
		ctx, registrationSubscriptionFlow, request, registrationSubscriptionTimeout, true,
	)
	if err != nil {
		return s.recordSubscriptionResult(nil, requestedExpires, fmt.Errorf("SUBSCRIBE transaction: %w", err))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		err = fmt.Errorf("SUBSCRIBE rejected with status %d (%s)", response.StatusCode, response.Reason)
		return s.recordSubscriptionResult(response, requestedExpires, err)
	}
	if err := s.recordSubscriptionResult(response, requestedExpires, nil); err != nil {
		return err
	}
	logging.Info("IMS SUBSCRIBE(reg) succeeded", "call_id", request.CallID().Value(), "code", response.StatusCode)
	return nil
}

func (s *Service) buildRegistrationSubscription() (*sip.Request, time.Duration, error) {
	profile, err := s.reserveSubscriptionSIPProfile()
	if err != nil {
		return nil, 0, fmt.Errorf("imscore: subscription registered profile: %w", err)
	}
	aor, err := parseSubscriptionURI(profile.LocalURI)
	if err != nil {
		return nil, 0, err
	}
	contact, err := buildSubscribeContactHeader(profile.ContactHeader, profile.Transport, true)
	if err != nil {
		return nil, 0, err
	}
	expires := registerExpires(s.cfg)
	options := subscribeRegHeaderOptions(subscribeRegRequestContext{
		profile: profile, aor: aor, contact: contact, expires: expires,
	})
	request, err := sipkit.BuildIMSRequest(sip.SUBSCRIBE, aor, options)
	return request, expires, err
}

type subscribeRegRequestContext struct {
	profile SIPDialogProfile
	aor     sip.Uri
	contact *sip.ContactHeader
	expires time.Duration
}

func subscribeRegHeaderOptions(requestContext subscribeRegRequestContext) sipkit.IMSRequestOptions {
	profile := requestContext.profile
	return sipkit.IMSRequestOptions{
		Destination: profile.RemoteAddress, Transport: profile.Transport,
		Branch: "z9hG4bK" + common.RandomHex(36), FromURI: requestContext.aor,
		FromTag: common.RandomHex(10), ToURI: requestContext.aor,
		CallID: common.RandomHex(20), CSeq: uint32(profile.InitialCSeq),
		Contact: requestContext.contact, Kind: sipkit.RequestKindOutOfDialog,
		SecurityMode: securityModeIPSec, AddRPort: true, OmitURITransport: true,
		AddUserAgent:      strings.TrimSpace(profile.UserAgent) != "",
		PreferredIdentity: imsheaders.PreferredIdentityHeaderValue(profile.LocalURI),
		Runtime: sipkit.IMSRuntimeSnapshot{
			ServiceRoute: profile.ServiceRoute, SecVerify: profile.SecurityVerify,
			PAccessNetworkInfo: profile.PANI, UserAgent: profile.UserAgent,
			LocalAddr: profile.LocalAddress, Transport: profile.Transport,
		},
		Headers: []sip.Header{
			sip.NewHeader("Expires", strconv.FormatInt(int64(requestContext.expires/time.Second), 10)),
			sip.NewHeader("Event", registrationEventPackage),
			sip.NewHeader("Accept", reginfoContentType),
		},
	}
}

func (s *Service) reserveSubscriptionSIPProfile() (SIPDialogProfile, error) {
	if s == nil || s.cfg == nil {
		return SIPDialogProfile{}, errors.New("service is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.regState != regRegistered || s.regSession == nil {
		return SIPDialogProfile{}, errors.New("registered SIP session is unavailable")
	}
	route := s.registeredSIPRouteLocked()
	if !route.live || route.clientAddress == "" || route.serverAddress == "" {
		return SIPDialogProfile{}, errors.New("registered SIP transport is unavailable")
	}
	if route.securityVerify == "" {
		return SIPDialogProfile{}, errors.New("protected registration security is unavailable")
	}
	localURI := firstNonBlank(s.regSession.publicID, s.reginfoAOR, primaryPublicIdentity(s.cfg))
	registeredContactUser := firstNonBlank(s.regSession.contactUser, contactUser(s.cfg))
	if localURI == "" || registeredContactUser == "" {
		return SIPDialogProfile{}, errors.New("registered subscription identity is unavailable")
	}
	minimum := s.regSession.cseq + 2
	if s.nextSIPCSeq < minimum {
		s.nextSIPCSeq = minimum
	} else {
		s.nextSIPCSeq++
	}
	contactURI, contactHeader := registeredVoiceContact(s.cfg, registeredContactUser, route.serverAddress)
	return SIPDialogProfile{
		LocalURI: localURI, FromTag: s.regSession.fromTag,
		ContactURI: contactURI, ContactHeader: contactHeader,
		LocalAddress: route.clientAddress, RemoteAddress: route.remoteAddress,
		Transport: route.transport, ServiceRoute: route.serviceRoute,
		SecurityVerify: route.securityVerify, PANI: s.GetPAccessNetworkInfo(),
		UserAgent: strings.TrimSpace(s.cfg.UserAgent), InitialCSeq: s.nextSIPCSeq,
	}, nil
}

func parseSubscriptionURI(value string) (sip.Uri, error) {
	var uri sip.Uri
	if err := sip.ParseUri(strings.TrimSpace(value), &uri); err != nil {
		return sip.Uri{}, fmt.Errorf("imscore: subscription AOR: %w", err)
	}
	return uri, nil
}

func buildSubscribeContactHeader(value, transport string, protected bool) (*sip.ContactHeader, error) {
	var uri sip.Uri
	params := sip.NewParams()
	displayName, err := sip.ParseAddressValue(strings.TrimSpace(value), &uri, &params)
	if err != nil {
		return nil, fmt.Errorf("imscore: subscription Contact: %w", err)
	}
	if protected {
		transport = "tcp"
	}
	if transport = strings.ToLower(strings.TrimSpace(transport)); transport != "" {
		uri.UriParams.Add("transport", transport)
	}
	return &sip.ContactHeader{DisplayName: displayName, Address: uri, Params: params}, nil
}

func (s *Service) recordSubscriptionAttempt(at time.Time, expires time.Duration) {
	s.mu.Lock()
	s.subscriptionLastAttemptAt = at
	s.subscriptionExpires = expires
	s.subscriptionRefreshAt = at.Add(subscriptionRefreshDelay(expires))
	s.subscriptionLastErr = ""
	s.mu.Unlock()
	s.signalIMSMaintenance()
}

func (s *Service) recordSubscriptionResult(
	response *sip.Response,
	requestedExpires time.Duration,
	resultErr error,
) error {
	completedAt := time.Now()
	s.mu.Lock()
	if resultErr != nil {
		s.subscriptionLastErr = resultErr.Error()
		s.mu.Unlock()
		return resultErr
	}
	expires := subscriptionExpires(response, requestedExpires)
	s.subscriptionLastOKAt = completedAt
	s.subscriptionExpires = expires
	s.subscriptionRefreshAt = completedAt.Add(subscriptionRefreshDelay(expires))
	s.subscriptionLastErr = ""
	s.mu.Unlock()
	s.signalIMSMaintenance()
	return nil
}

func subscriptionRefreshDelay(expires time.Duration) time.Duration {
	if expires > imsRegistrationRefreshAdvance {
		return expires - imsRegistrationRefreshAdvance
	}
	return 0
}

func subscriptionExpires(response *sip.Response, fallback time.Duration) time.Duration {
	if response == nil {
		return fallback
	}
	value := sipkit.FirstHeaderValue(response, "Expires", true)
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func subscriptionRuntimeError(err error) error {
	return fmt.Errorf("imscore: registration event subscription failed: %w", err)
}

func firstSIPHeaderURI(value string) string {
	value = strings.TrimSpace(value)
	if start := strings.IndexByte(value, '<'); start >= 0 {
		if end := strings.IndexByte(value[start+1:], '>'); end >= 0 {
			return strings.TrimSpace(value[start+1 : start+1+end])
		}
	}
	value, _, _ = strings.Cut(value, ",")
	return strings.Trim(strings.TrimSpace(value), "<>")
}
