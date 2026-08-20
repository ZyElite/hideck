package imscore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsheaders"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

const maxAKAChallenges = 3

// registerSession tracks one registration attempt.
type registerSession struct {
	callID       string
	fromTag      string
	contactUser  string
	cseq         int
	branch       string
	challenge    *DigestChallenge
	authHeader   string
	expires      time.Duration
	security     *securityAgreement
	publicID     string
	serviceRoute string
	path         string
	template     policy.IMSRegisterTemplate
}

type registerAttemptResult struct {
	statusCode     int
	challengeCount int
	retryAfter     time.Duration
	minExpires     uint32
	secAgree       bool
	authRealm      string
	authorization  string
	securityVerify string
}

type registerAttemptError struct {
	result registerAttemptResult
	err    error
}

type registerRequestOptions struct {
	unregister bool
	wildcard   bool
}

func (e *registerAttemptError) Error() string { return e.err.Error() }
func (e *registerAttemptError) Unwrap() error { return e.err }

// Register performs the IMS registration flow (RFC 3261 + Digest-AKA).
func (s *Service) Register(ctx context.Context) error {
	ctx = common.WithTraceID(ctx, common.TraceID(ctx))
	s.registerMu.Lock()
	defer s.registerMu.Unlock()
	select {
	case <-s.stop:
		return errors.New("imscore: service stopped")
	default:
	}
	s.mu.Lock()
	s.regState = regRegistering
	s.lastRegisterTraceID = common.TraceID(ctx)
	s.lastRegisterAttemptAt = time.Now()
	s.mu.Unlock()

	var expires time.Duration
	var err error
	if s.bindingCleanupPending.Swap(false) {
		err = s.clearRegistrationBindingsLocked(ctx)
		if err != nil {
			logging.Info("IMS registration binding cleanup flow failed",
				"device", s.DeviceID(), "err", err)
		}
	}
	if err == nil {
		expires, err = s.runRegisterFlow(ctx)
	}
	if err != nil {
		s.mu.Lock()
		s.regState = regFailed
		s.lastError = err.Error()
		s.lastRegisterErr = err.Error()
		s.signalingReady = false
		s.signalingFailureReason = err.Error()
		s.mu.Unlock()
		if !isRegisterOperationCanceled(err) {
			s.applyRegistrationFailureStatus(err)
		}
		s.notifySMSReadiness()
		return err
	}
	s.mu.Lock()
	s.regState = regRegistered
	s.lastError = ""
	s.lastRegisterErr = ""
	s.lastRegisterOKAt = time.Now()
	transport := s.registrationTransport
	protectedTCP := s.registrationTCP != nil && s.registrationTCPProtected
	publicID := ""
	secAgree := false
	if s.regSession != nil {
		publicID = s.regSession.publicID
		secAgree = s.regSession.security != nil && strings.TrimSpace(s.regSession.security.verifyHeader) != ""
	}
	s.mu.Unlock()
	logging.Info("IMS REGISTER succeeded",
		"device", s.DeviceID(),
		"expires_seconds", int(expires/time.Second),
		"transport", transport,
		"protected_tcp", protectedTCP,
		"sec_agree", secAgree,
		"public_id", loggablePublicID(publicID))
	s.transitionRegStatus(registrationRegistered)
	s.regFailCount.Store(0)
	s.reRegisterPending.Store(false)
	s.markSignalingReady()
	if s.onRegistered != nil {
		s.onRegistered()
	}
	s.notifySMSReadiness()
	s.scheduleRegistrationRefresh(expires)
	s.startRegistrationSubscription()
	s.startIMSKeepalive()
	return nil
}

// runRegisterFlow drives the REGISTER -> 401/407 -> REGISTER flow.
func (s *Service) runRegisterFlow(ctx context.Context) (time.Duration, error) {
	if s.cfg == nil {
		return 0, errors.New("imscore: no configuration")
	}
	if err := s.ensureRegistrationTransport(ctx); err != nil {
		return 0, err
	}
	candidates := registerTransportCandidates(s.cfg.Transport)
	transportIndex := indexOfRegisterTransport(candidates, s.currentRegistrationTransport())
	minExpiresRetried := false
	for {
		expires, result, err := s.runRegisterAttempt(ctx)
		if err == nil {
			return expires, nil
		}
		if result.statusCode == 423 && result.minExpires > 0 && !minExpiresRetried {
			s.applyRegisterMinExpires(result.minExpires)
			minExpiresRetried = true
			continue
		}
		if s.usesExternalRegistrationTransport() || !shouldRetryNextRegisterTransport(result, err) {
			return 0, &registerAttemptError{result: result, err: err}
		}
		next, switchErr := s.replaceInitialRegistrationTransport(ctx, candidates, transportIndex+1)
		if switchErr != nil {
			attemptErr := &registerAttemptError{result: result, err: err}
			return 0, errors.Join(attemptErr, switchErr)
		}
		transportIndex = next
	}
}

func (s *Service) runRegisterAttempt(ctx context.Context) (time.Duration, registerAttemptResult, error) {
	var result registerAttemptResult
	session, err := s.sessionForRegisterAttempt()
	if err != nil {
		return 0, result, err
	}
	s.mu.Lock()
	s.regSession = session
	s.mu.Unlock()
	s.recordRegisterSession(session)
	response, result, err := s.exchangeRegisterChallenges(ctx, session)
	if err != nil {
		return 0, result, err
	}
	expires, err := s.commitRegisterSuccess(response, session)
	return expires, result, err
}

func (s *Service) exchangeRegisterChallenges(
	ctx context.Context,
	session *registerSession,
) (*sipResponse, registerAttemptResult, error) {
	var result registerAttemptResult
	resp, err := s.exchangeRegister(ctx, session, session.authHeader)
	if err != nil {
		return nil, result, err
	}
	updateRegisterAttemptResponse(&result, resp)
	resp, err = s.retryDefaultInitialRegister(ctx, session, resp)
	if err != nil {
		return nil, result, err
	}
	updateRegisterAttemptResponse(&result, resp)
	for challengeCount := 0; isDigestChallengeResponse(resp); challengeCount++ {
		result.challengeCount = challengeCount + 1
		if challengeCount >= maxAKAChallenges {
			return nil, result, fmt.Errorf("imscore: AKA challenge limit %d exceeded", maxAKAChallenges)
		}
		auth, syncFailure, err := s.answerDigestChallenge(ctx, session, resp)
		if err != nil {
			return nil, result, err
		}
		updateRegisterAttemptAuth(&result, session, auth)
		session.cseq++
		session.authHeader = auth
		resp, err = s.exchangeRegister(ctx, session, auth)
		if err != nil {
			return nil, result, err
		}
		updateRegisterAttemptResponse(&result, resp)
		if syncFailure && !isDigestChallengeResponse(resp) {
			return nil, result, fmt.Errorf("imscore: AKA synchronization response status %d did not provide a fresh challenge", resp.StatusCode)
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, result, registrationResponseError(resp, session.challenge != nil)
	}
	if err := s.finalizeInitialRegisterSecurity(session, resp); err != nil {
		return nil, result, err
	}
	return resp, result, nil
}

func (s *Service) finalizeInitialRegisterSecurity(session *registerSession, response *sipResponse) error {
	if session.security == nil {
		s.mu.RLock()
		modeRecorded := s.effectiveSecurityMode != ""
		s.mu.RUnlock()
		if !modeRecorded {
			s.recordSecurityMode(securityModePlain, "", false)
		}
		return nil
	}
	if session.security.server != nil {
		return nil
	}
	if err := decideInitialRegisterSuccessSecurity(
		s.cfg, session.template, strings.TrimSpace(response.Header("Security-Server")) != "",
	); err != nil {
		s.recordSignalingFailure(securityModeIPSec, err.Error(), err)
		return fmt.Errorf("imscore: %w", err)
	}
	s.releaseUnusedProtectedReservations()
	session.security = nil
	s.recordSecurityMode(securityModePlain, "", false)
	return nil
}

func updateRegisterAttemptResponse(result *registerAttemptResult, response *sipResponse) {
	result.statusCode = response.StatusCode
	result.retryAfter, result.minExpires = parseRegisterRetryHintsFromResponse(response)
}

func updateRegisterAttemptAuth(result *registerAttemptResult, session *registerSession, authorization string) {
	result.authorization = authorization
	result.secAgree = session.security != nil
	if session.security != nil {
		result.securityVerify = session.security.verifyHeader
	}
	if session.challenge != nil {
		result.authRealm = session.challenge.Realm
	}
}

func (s *Service) commitRegisterSuccess(resp *sipResponse, session *registerSession) (time.Duration, error) {
	s.finalizeRegistrationTransportSwitch()
	expires := registrationExpires(resp, s.cfg.Expires)
	session.expires = expires
	s.recordRegisterSession(session)
	s.recordRegisterGRUU(resp.Header("Contact"))
	associatedURI := resp.Header("P-Associated-URI")
	if publicID := associatedPublicIdentity(associatedURI); publicID != "" {
		session.publicID = publicID
	}
	if number := imsheaders.ExtractPhoneFromAssociatedMSISDN(associatedURI); number != "" {
		s.mu.Lock()
		s.assocMSISDN = number
		s.mu.Unlock()
		s.publishLocalNumberLearned(number, "p-associated-uri")
	}
	if session.publicID != "" {
		s.mu.Lock()
		s.learnedAOR = session.publicID
		s.mu.Unlock()
	}
	if serviceRoute := imsheaders.FirstRoute(resp.Header("Service-Route"), ""); serviceRoute != "" {
		session.serviceRoute = serviceRoute
		s.mu.Lock()
		s.serviceRoute = serviceRoute
		s.mu.Unlock()
	}
	if path := strings.TrimSpace(resp.Header("Path")); path != "" {
		session.path = path
		s.mu.Lock()
		s.path = path
		s.mu.Unlock()
	}
	return expires, nil
}

func registerAttemptReachedAuthPhase(result registerAttemptResult) bool {
	return result.challengeCount > 0 || result.secAgree || strings.TrimSpace(result.securityVerify) != "" ||
		strings.TrimSpace(result.authRealm) != "" || strings.TrimSpace(result.authorization) != ""
}

func shouldRetryNextRegisterTransport(result registerAttemptResult, err error) bool {
	if isRegisterOperationCanceled(err) || registerAttemptReachedAuthPhase(result) {
		return false
	}
	return result.statusCode == 0 || result.statusCode == 408 ||
		(result.statusCode >= 502 && result.statusCode <= 504)
}

func isRegisterOperationCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func indexOfRegisterTransport(candidates []string, current string) int {
	for index, candidate := range candidates {
		if candidate == current {
			return index
		}
	}
	return 0
}

func (s *Service) currentRegistrationTransport() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.registrationTransport
}

func (s *Service) usesExternalRegistrationTransport() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.externalTransport
}

func (s *Service) applyRegistrationFailureStatus(err error) {
	if errors.Is(err, enginesim.ErrAPDUBusy) {
		s.handleRegisterAPDUBusy(err)
		return
	}
	var responseErr *registerResponseError
	var attemptErr *registerAttemptError
	result := registerAttemptResult{}
	if errors.As(err, &attemptErr) {
		result = attemptErr.result
	}
	errors.As(err, &responseErr)
	transportFailure := result.statusCode == 0
	if responseErr != nil {
		result.statusCode = responseErr.statusCode
		result.retryAfter = responseErr.retryAfter
		result.minExpires = responseErr.minExpires
	}
	outcome := decideRegisterFailureOutcome(
		time.Now(), result, s.cfg.IMSRegisterTemplate.RegisterPolicy, transportFailure,
	)
	logging.Info("IMS REGISTER failure",
		"device", s.DeviceID(),
		"status", result.statusCode, "reason", outcome.reason,
		"err", err,
		"register_policy", effectiveRegisterPolicyID(s.cfg.IMSRegisterTemplate, s.cfg.IMSRegisterPolicySource),
		"register_policy_source", normalizeRegisterPolicySource(s.cfg.IMSRegisterPolicySource))
	s.regFailCount.Add(1)
	s.reRegisterPending.Store(outcome.kind == registrationRejectedTemporary)
	s.mu.Lock()
	s.nextRegister = outcome.nextRegister
	s.mu.Unlock()
	if responseErr != nil {
		s.lastSIPCode.Store(int32(responseErr.statusCode))
	}
	s.transitionRegStatus(outcome.kind)
	if outcome.kind == registrationRejectedTemporary && outcome.reason != "min_expires" {
		if s.advanceRegistrarForNextRetry(outcome.reason) {
			s.resetRegistrationTransportForRegistrarRetry()
		}
	}
	s.triggerRegisterReconnect()
}

func (s *Service) applyRegisterMinExpires(seconds uint32) {
	duration := time.Duration(seconds) * time.Second
	if s.cfg.Expires < duration {
		s.cfg.Expires = duration
	}
	if s.cfg.RegisterTemplate.Expires < duration {
		s.cfg.RegisterTemplate.Expires = duration
	}
	if seconds > uint32(s.cfg.IMSRegisterTemplate.Expires) {
		s.cfg.IMSRegisterTemplate.Expires = int(seconds)
	}
}

func (s *Service) exchangeRegister(ctx context.Context, session *registerSession, authorization string) (*sipResponse, error) {
	s.recordRegisterSession(session)
	request := s.buildRegister(session, authorization)
	logging.Debug("IMS REGISTER outbound", "device", s.DeviceID(), "cseq", session.cseq,
		"authenticated", strings.TrimSpace(authorization) != "",
		"security_client_mechanisms", securityClientMechanismCount(rawSIPHeaderValue(request, "Security-Client")),
		"sip", logging.RedactSIPRaw(request))
	response, err := s.transport.RoundTrip(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("imscore: REGISTER CSeq %d transaction: %w", session.cseq, err)
	}
	matched, matchErr := matchesRegisterTransaction(response, session)
	if matchErr != nil {
		return nil, matchErr
	}
	if !matched {
		return nil, fmt.Errorf("imscore: REGISTER CSeq %d received mismatched transaction response", session.cseq)
	}
	logging.Info("IMS REGISTER response",
		"device", s.DeviceID(), "cseq", session.cseq, "status", response.StatusCode,
		"security_server", response.Header("Security-Server") != "",
		"digest_challenge", isDigestChallengeResponse(response))
	s.logRegisterFlowNegotiation(response)
	s.recordRegisterResponse(response)
	return response, nil
}

func isDigestChallengeResponse(response *sipResponse) bool {
	return response != nil && (response.StatusCode == 401 || response.StatusCode == 407)
}

func (s *Service) answerDigestChallenge(ctx context.Context, session *registerSession, response *sipResponse) (string, bool, error) {
	updateAuthResponseRouteSecurity(session, response)
	challenge, err := s.extractChallenge(response, response.StatusCode)
	if err != nil {
		return "", false, err
	}
	session.challenge = challenge
	s.mu.Lock()
	s.challengeRealm = challenge.Realm
	s.authRealm = pickAuthRealm(s.cfg.Realm, challenge.Realm, s.cfg.Domain)
	s.mu.Unlock()
	logging.RunDebug("IMS Digest-AKA challenge", "cseq", session.cseq,
		"algorithm", challenge.Algorithm, "qop", challenge.QOP)
	authorization, aka, authErr := s.buildAuthorizationWithResult(session)
	syncFailure := errors.Is(authErr, enginesim.ErrSyncFailure)
	if authErr != nil && !syncFailure {
		return "", false, authErr
	}
	if session.security != nil && !syncFailure {
		if err := s.installNegotiatedIPSec(ctx, session, response, aka); err != nil {
			return "", false, err
		}
	}
	return authorization, syncFailure, nil
}

func (s *Service) sessionForRegisterAttempt() (*registerSession, error) {
	s.mu.RLock()
	previous := s.regSession
	if previous != nil && previous.expires > 0 {
		session := &registerSession{
			callID: previous.callID, fromTag: previous.fromTag,
			contactUser: previous.contactUser,
			cseq:        previous.cseq + 1, challenge: previous.challenge,
			authHeader: previous.authHeader, expires: previous.expires,
			security: previous.security, publicID: previous.publicID,
			serviceRoute: previous.serviceRoute, path: previous.path, template: previous.template,
		}
		s.mu.RUnlock()
		return session, nil
	}
	s.mu.RUnlock()
	template := resolveActiveIMSRegisterTemplate(s.cfg)
	security, err := s.prepareSecurityAgreement(template)
	if err != nil {
		return nil, err
	}
	return &registerSession{
		callID: newCallID(), fromTag: newTag(), contactUser: newUUID(),
		cseq: 2, security: security, template: template,
	}, nil
}

func (s *Service) retryDefaultInitialRegister(
	ctx context.Context,
	session *registerSession,
	response *sipResponse,
) (*sipResponse, error) {
	template := session.template
	if !template.EnableInitialRejectFallback || response == nil {
		return response, nil
	}
	warning, body, _, _ := summarizeSIPFailure(response)
	retryPolicy := defaultRegisterRetryPolicy(template.RegisterPolicy)
	if !retryPolicy.ShouldRetryDefaultInitial(response.StatusCode, warning, body) {
		return response, nil
	}
	session.template = policy.FallbackIMSRegisterTemplate(template)
	if session.security != nil {
		session.security.clientHeader = securityClientHeaderValue(session.security.client, session.template)
	}
	session.cseq++
	return s.exchangeRegister(ctx, session, session.authHeader)
}

func (s *Service) recordRegisterSession(session *registerSession) {
	if session == nil {
		return
	}
	s.mu.Lock()
	s.callID = session.callID
	s.fromTag = session.fromTag
	s.expires = uint32(session.expires / time.Second)
	s.mu.Unlock()
	s.cseq.Store(uint32(session.cseq))
}

func (s *Service) recordRegisterResponse(response *sipResponse) {
	if response == nil {
		return
	}
	s.logRegisterSMSCapabilityTrace(response)
	s.lastSIPCode.Store(int32(response.StatusCode))
	text := strings.TrimSpace(response.Reason)
	if text == "" {
		text = SIPStatusText(response.StatusCode)
	}
	s.mu.Lock()
	s.lastSIPText = text
	s.mu.Unlock()
}

// buildRegister builds a REGISTER request.
func (s *Service) buildRegister(session *registerSession, authHeader string) string {
	return s.buildRegisterRequest(session, authHeader, registerRequestOptions{})
}

// buildContactUnregister removes only the current Contact binding.
func (s *Service) buildContactUnregister(session *registerSession, authHeader string) string {
	return s.buildRegisterRequest(session, authHeader, registerRequestOptions{unregister: true})
}

// buildWildcardUnregister builds an authenticated REGISTER that removes all
// bindings for the current public identity.
func (s *Service) buildWildcardUnregister(session *registerSession, authHeader string) string {
	return s.buildRegisterRequest(session, authHeader, registerRequestOptions{
		unregister: true,
		wildcard:   true,
	})
}

func (s *Service) buildRegisterRequest(
	session *registerSession,
	authHeader string,
	options registerRequestOptions,
) string {
	cfg := s.cfg
	// Each request starts a distinct SIP transaction even when refresh reuses
	// the registration Call-ID.
	session.branch = "z9hG4bK" + newBranch()
	expires := registerExpires(cfg)
	if options.unregister {
		expires = 0
	}
	contact := registerContact(s.registerContactOptions(session), s.registerRequestTransport(
		registerUsesProtectedTransport(session),
	), int(expires.Seconds()))
	if options.wildcard {
		contact = "*"
	} else if options.unregister && !strings.Contains(strings.ToLower(contact), ";expires=") {
		contact += ";expires=0"
	}
	authenticated := strings.TrimSpace(authHeader) != ""
	protected := registerUsesProtectedTransport(session)
	transport := s.registerRequestTransport(protected)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("REGISTER sip:%s SIP/2.0\r\n", cfg.Domain))
	localAddress := s.registerLocalAddress(session)
	b.WriteString(fmt.Sprintf("Via: SIP/2.0/%s %s;rport;branch=%s%s\r\n",
		transportUpper(transport), localAddress, session.branch, registerViaAlias(protected)))
	publicIdentity := primaryPublicIdentity(cfg)
	b.WriteString(fmt.Sprintf("From: <%s>;tag=%s\r\n", publicIdentity, session.fromTag))
	b.WriteString(fmt.Sprintf("To: <%s>\r\n", publicIdentity))
	b.WriteString(fmt.Sprintf("Call-ID: %s\r\n", session.callID))
	b.WriteString(fmt.Sprintf("CSeq: %d REGISTER\r\n", session.cseq))
	b.WriteString("Contact: " + contact + "\r\n")
	b.WriteString(fmt.Sprintf("Expires: %d\r\n", int(expires.Seconds())))
	b.WriteString("Max-Forwards: 70\r\n")
	b.WriteString("Supported: " + formatHeaderList(registerSupportedHeaderForSession(cfg, session)) + "\r\n")
	if allow := registerConfiguredAllowHeader(cfg); allow != "" {
		b.WriteString("Allow: " + allow + "\r\n")
	}
	if registerIncludesPANI(cfg.RegisterTemplate, authenticated || protected) {
		b.WriteString("P-Access-Network-Info: " +
			imsheaders.PAccessNetworkInfo(s.GetPAccessNetworkInfo()) + "\r\n")
	}
	if cellular := strings.TrimSpace(cfg.CellularNetworkInfo); cellular != "" {
		b.WriteString("Cellular-Network-Info: " + cellular + "\r\n")
	}
	b.WriteString(registerSecurityHeaders(session))
	if route := strings.TrimSpace(session.serviceRoute); route != "" {
		b.WriteString("Route: " + route + "\r\n")
	}
	if authHeader == "" {
		authHeader = initialIMSAuthorization(cfg)
	}
	b.WriteString("Authorization: " + authHeader + "\r\n")
	if strings.TrimSpace(cfg.UserAgent) != "" {
		b.WriteString("User-Agent: " + strings.TrimSpace(cfg.UserAgent) + "\r\n")
	}
	b.WriteString("Content-Length: 0\r\n\r\n")
	return b.String()
}

func (s *Service) registerContactOptions(session *registerSession) imsheaders.ContactOptions {
	options := imsheaders.ContactOptions{
		ContactID: session.contactUser, LocalAddr: s.cfg.LocalIP.String(),
		LocalPortC: s.cfg.LocalPort, LocalPortS: s.cfg.LocalPort,
		AccessType:        registerConfiguredAccessType(s.cfg),
		ContactParamOrder: s.cfg.RegisterTemplate.ContactOrder,
		SIPInstance:       s.cfg.IMEI, IcsiRef: s.cfg.RegisterTemplate.ICSIRef,
		IMEI: s.cfg.DeviceID,
	}
	if strings.TrimSpace(options.ContactID) == "" {
		options.ContactID = contactUser(s.cfg)
	}
	if session.security != nil {
		options.LocalPortC = int(session.security.client.PortC)
		options.LocalPortS = int(session.security.client.PortS)
	}
	return options
}

func registerContact(options imsheaders.ContactOptions, transport string, expires int) string {
	legacyParams := len(options.ContactParamOrder) == 0 && len(options.ParamOrder) == 0
	if legacyParams {
		options.ContactParamOrder = []string{"sip_instance"}
	}
	uri := imsheaders.ContactURI(options, transport)
	var builder strings.Builder
	builder.WriteByte('<')
	builder.WriteString(uri)
	builder.WriteByte('>')
	for _, param := range imsheaders.ContactParams(options) {
		builder.WriteByte(';')
		builder.WriteString(param.Name)
		if param.Value != "" {
			builder.WriteByte('=')
			builder.WriteString(param.Value)
		}
	}
	if legacyParams && expires >= 0 {
		builder.WriteString(fmt.Sprintf(";expires=%d", expires))
	}
	return builder.String()
}

func registerExpires(cfg *IMSConfig) time.Duration {
	if cfg.IMSRegisterTemplate.Expires > 0 {
		return time.Duration(registerExpiresForTemplate(
			cfg.IMSRegisterTemplate, int(cfg.Expires/time.Second),
		)) * time.Second
	}
	if cfg.RegisterTemplate.Expires > 0 {
		return cfg.RegisterTemplate.Expires
	}
	if cfg.Expires > 0 {
		return cfg.Expires
	}
	return 3600 * time.Second
}

func registerConfiguredAllowHeader(cfg *IMSConfig) string {
	if cfg.IMSRegisterTemplate.ID != "" || cfg.IMSRegisterTemplate.AllowHeader != "" {
		return registerAllowHeader(cfg.IMSRegisterTemplate)
	}
	return strings.TrimSpace(cfg.RegisterTemplate.AllowHeader)
}

func registerConfiguredAccessType(cfg *IMSConfig) string {
	if cfg.IMSRegisterTemplate.ID != "" || cfg.IMSRegisterTemplate.AccessType != "" {
		return registerAccessType(cfg.RegisterTemplate.AccessType, cfg.IMSRegisterTemplate)
	}
	return strings.TrimSpace(cfg.RegisterTemplate.AccessType)
}

func registerSupportedHeader(cfg *IMSConfig) string {
	if supported := strings.TrimSpace(cfg.RegisterTemplate.SupportedHeader); supported != "" {
		return supported
	}
	return "path, outbound"
}

func registerSupportedHeaderForSession(cfg *IMSConfig, session *registerSession) string {
	supported := registerSupportedHeader(cfg)
	if session != nil {
		if session.template.ID == "" && session.template.SecAgreeMode == "" &&
			len(session.template.SecurityClientMechanisms) == 0 {
			return supported
		}
		template := policy.NormalizeIMSRegisterTemplate(session.template)
		supported = template.SupportedHeader
		if template.ID == compatibilityRegisterTemplateID {
			return supported
		}
	}
	if session == nil || session.security == nil {
		supported = removeHeaderToken(supported, "sec-agree")
	}
	return supported
}

func removeHeaderToken(value, removed string) string {
	parts := strings.Split(value, ",")
	result := parts[:0]
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && !strings.EqualFold(part, removed) {
			result = append(result, part)
		}
	}
	return strings.Join(result, ",")
}

func initialIMSAuthorization(cfg *IMSConfig) string {
	realm := pickAuthRealm(cfg.Realm, "", cfg.Domain)
	return fmt.Sprintf(`Digest uri="sip:%s",username="%s",response="",realm="%s",nonce=""`,
		digestQuotedValue(cfg.Domain), digestQuotedValue(cfg.IMPI), digestQuotedValue(realm))
}

func digestQuotedValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(strings.TrimSpace(value))
}

func registerSecurityHeaders(session *registerSession) string {
	if session == nil || session.security == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("Security-Client: " + session.security.clientHeader + "\r\n")
	headers := imsheaders.SecAgreeProtectedHeaders(session.security.verifyHeader)
	if len(headers) == 0 {
		headers = []imsheaders.Header{
			{Name: "Require", Value: "sec-agree"},
			{Name: "Proxy-Require", Value: "sec-agree"},
		}
	}
	for _, header := range headers {
		builder.WriteString(header.Name + ": " + header.Value + "\r\n")
	}
	return builder.String()
}

func associatedPublicIdentity(header string) string {
	identity := imsheaders.PreferredIdentityHeaderValue(imsheaders.PickAssociatedMSISDN(header))
	if len(identity) >= 2 && identity[0] == '<' && identity[len(identity)-1] == '>' {
		return identity[1 : len(identity)-1]
	}
	return identity
}

func (s *Service) registerLocalAddress(_ *registerSession) string {
	return net.JoinHostPort(s.cfg.LocalIP.String(), strconv.Itoa(s.cfg.LocalPort))
}

func (s *Service) registerRequestTransport(protected bool) string {
	if protected {
		return "tcp"
	}
	s.mu.RLock()
	transport := s.registrationTransport
	s.mu.RUnlock()
	if transport != "" {
		return transport
	}
	candidates := registerTransportCandidates(s.cfg.Transport)
	return candidates[0]
}

func registerViaAlias(protected bool) string {
	if protected {
		return ";alias"
	}
	return ""
}

func registerUsesProtectedTransport(session *registerSession) bool {
	return session != nil && session.security != nil && session.security.verifyHeader != ""
}

func registerIncludesPANI(template IMSRegisterTemplate, authenticated bool) bool {
	return !template.IncludePANIAuthenticated || authenticated
}

func formatHeaderList(value string) string {
	parts := strings.Split(value, ",")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return strings.Join(parts, ", ")
}

type registerResponseError struct {
	statusCode int
	retryAfter time.Duration
	minExpires uint32
	message    string
}

func (e *registerResponseError) Error() string { return e.message }

func registrationResponseError(response *sipResponse, challenged bool) error {
	phase := "initial REGISTER"
	if challenged {
		phase = "authenticated REGISTER"
	}
	detail := strings.TrimSpace(response.Reason)
	retryAfter, minExpires := parseRegisterRetryHintsFromResponse(response)
	if warning := strings.TrimSpace(response.Header("Warning")); warning != "" {
		if detail != "" {
			detail += "; "
		}
		detail += "warning=" + warning
	}
	if detail == "" {
		return &registerResponseError{statusCode: response.StatusCode, retryAfter: retryAfter, minExpires: minExpires, message: fmt.Sprintf(
			"imscore: registration failed during %s with status %d", phase, response.StatusCode)}
	}
	return &registerResponseError{statusCode: response.StatusCode, retryAfter: retryAfter, minExpires: minExpires, message: fmt.Sprintf(
		"imscore: registration failed during %s with status %d (%s)", phase, response.StatusCode, detail)}
}

func primaryPublicIdentity(cfg *IMSConfig) string {
	if identity := firstNonBlank(cfg.publicIdentities()...); identity != "" {
		if strings.Contains(identity, ":") {
			return identity
		}
		user, domain, _ := strings.Cut(identity, "@")
		return formatAORForSIP("sip", user, domain)
	}
	user, domain, _ := strings.Cut(cfg.IMPI, "@")
	return formatAORForSIP("sip", user, domain)
}

func contactUser(cfg *IMSConfig) string {
	if strings.TrimSpace(cfg.IMSI) != "" {
		return strings.TrimSpace(cfg.IMSI)
	}
	user, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(cfg.IMPI), "sip:"), "@")
	return user
}

func registrationExpires(resp *sipResponse, configured time.Duration) time.Duration {
	fallback := uint32(0)
	if configured > 0 {
		fallback = uint32(configured / time.Second)
	}
	seconds := parseRegisterExpiresFromResponse(resp, fallback)
	if seconds == 0 {
		seconds = uint32(time.Hour / time.Second)
	}
	return time.Duration(seconds) * time.Second
}

func contactExpires(contact string) int {
	for _, parameter := range strings.Split(contact, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
		if !ok || !strings.EqualFold(name, "expires") {
			continue
		}
		value, _, _ = strings.Cut(value, ",")
		seconds, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && seconds > 0 {
			return seconds
		}
	}
	return 0
}

func (s *Service) scheduleRegistrationRefresh(expires time.Duration) {
	delay := registrationRefreshDelay(expires)
	s.mu.Lock()
	now := time.Now()
	s.registrationRefreshAt = nextRegisterAtAfterSuccess(now, delay)
	s.registrationRuntime.expires = uint32(expires / time.Second)
	s.nextRegister = nextRegisterAtAfterSuccess(now, delay)
	s.mu.Unlock()
	s.signalIMSMaintenance()
}

func (s *Service) refreshRegistration() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Register(ctx); err != nil {
		logRegisterRetryAttemptFailure(s.DeviceID(), "periodic", err)
		s.reportRegistrationRuntimeError(fmt.Errorf("imscore: registration refresh failed: %w", err))
	}
}

func sipLocalAddress(cfg *IMSConfig) string {
	port := cfg.LocalPort
	if port <= 0 {
		port = 5060
	}
	return net.JoinHostPort(cfg.LocalIP.String(), strconv.Itoa(port))
}

func loggablePublicID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if index := strings.LastIndexByte(value, '@'); index >= 0 && index+1 < len(value) {
		return "***@" + strings.Trim(value[index+1:], ">")
	}
	return "***"
}

// extractChallenge extracts the digest challenge from a 401/407 response.
func (s *Service) extractChallenge(resp *sipResponse, statusCode int) (*DigestChallenge, error) {
	header, value := pickAuthHeader(resp)
	if value == "" {
		header = "WWW-Authenticate"
		if statusCode == 407 {
			header = "Proxy-Authenticate"
		}
		return nil, errors.New("imscore: challenge response missing " + header)
	}
	return ParseDigestChallenge(value)
}

// buildAuthorization computes the Authorization header for the session.
func (s *Service) buildAuthorization(session *registerSession) (string, error) {
	authorization, _, err := s.buildAuthorizationWithResult(session)
	return authorization, err
}

func (s *Service) buildAuthorizationWithResult(session *registerSession) (string, AKAResult, error) {
	cfg := s.cfg
	if cfg.AKAProvider == nil {
		return "", AKAResult{}, errors.New("imscore: no AKA provider for digest")
	}
	if session.challenge == nil {
		return "", AKAResult{}, errors.New("imscore: no challenge for digest")
	}
	uri := "sip:" + cfg.Domain
	return ProcessAKAChallengeWithResult(session.challenge, cfg.AKAProvider, cfg.IMPI, "REGISTER", uri)
}

// Transport returns the SIP transport (for tests and wiring).
func (s *Service) Transport() *sipTransport {
	if s == nil {
		return nil
	}
	return s.transport
}

// SendRawSIP sends a raw SIP request through the transport (used by the
// voice layer for INVITE/BYE/ACK/CANCEL).
func (s *Service) SendRawSIP(req string) error {
	if s == nil {
		return errors.New("imscore: nil service")
	}
	return s.sendSIP(req)
}

// sendSIP sends a SIP request through the transport.
func (s *Service) sendSIP(req string) error {
	if s.transport == nil {
		return errors.New("imscore: no SIP transport")
	}
	return s.transport.Send(req)
}

// receiveResponse waits for a response matching the full REGISTER transaction.
func (s *Service) receiveResponse(ctx context.Context, session *registerSession) (*sipResponse, error) {
	if s.transport == nil {
		return nil, errors.New("imscore: no SIP transport")
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case resp := <-s.transport.Responses():
			matched, err := matchesRegisterTransaction(resp, session)
			if err != nil {
				return nil, err
			}
			if !matched || resp.StatusCode < 200 {
				continue
			}
			return resp, nil
		}
	}
}

func matchesRegisterTransaction(resp *sipResponse, session *registerSession) (bool, error) {
	if resp == nil || resp.CallID != session.callID {
		return false, nil
	}
	cseq, method, err := parseSIPCSeq(resp.CSeq)
	if err != nil {
		return false, fmt.Errorf("imscore: invalid REGISTER response CSeq: %w", err)
	}
	if cseq != session.cseq || !strings.EqualFold(method, "REGISTER") {
		return false, nil
	}
	branch, err := parseTopViaBranch(resp.Header("Via"))
	if err != nil {
		return false, fmt.Errorf("imscore: invalid REGISTER response Via: %w", err)
	}
	return branch == session.branch, nil
}

func parseSIPCSeq(value string) (int, string, error) {
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return 0, "", errors.New("expected sequence number and method")
	}
	sequence, err := strconv.Atoi(fields[0])
	if err != nil || sequence < 0 {
		return 0, "", fmt.Errorf("invalid sequence number %q", fields[0])
	}
	return sequence, fields[1], nil
}

func parseTopViaBranch(value string) (string, error) {
	topVia, _, _ := strings.Cut(value, ",")
	for _, parameter := range strings.Split(topVia, ";")[1:] {
		name, branch, ok := strings.Cut(strings.TrimSpace(parameter), "=")
		if ok && strings.EqualFold(name, "branch") && strings.TrimSpace(branch) != "" {
			return strings.TrimSpace(branch), nil
		}
	}
	return "", errors.New("missing branch parameter")
}

// newCallID generates a call ID.
func newCallID() string {
	return newUUID()
}

// newTag generates a tag.
func newTag() string {
	return randomHex(8)
}

// newBranch generates a Via branch.
func newBranch() string {
	return randomHex(16)
}

func newUUID() string {
	raw := make([]byte, 16)
	_, _ = randRead(raw)
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	encoded := fmt.Sprintf("%x", raw)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}

// randomHex generates a hexadecimal string with exactly n characters.
func randomHex(n int) string {
	return common.RandomHex(n)
}

// transportUpper upper-cases a transport token.
func transportUpper(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "tcp":
		return "TCP"
	case "tls":
		return "TLS"
	default:
		return "UDP"
	}
}

// formatHostPort formats an IP:port for SIP.
func formatHostPort(ip interface{ String() string }) string {
	return ip.String()
}
