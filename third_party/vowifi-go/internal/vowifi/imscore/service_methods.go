package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/ipsec3gpp"
)

// Start launches the IMS core session.
func (s *Service) Start(ctx context.Context) error {
	if s == nil || s.cfg == nil {
		return errors.New("imscore: service not configured")
	}
	return s.Register(ctx)
}

// Snapshot returns a map snapshot of the IMS context.
func (s *Service) Snapshot() map[string]interface{} {
	return s.GetIMSContextSnapshot()
}

// Session returns the IMS session state string.
func (s *Service) Session() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// ToMap converts a service status to a map.
func (st ServiceStatus) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"enabled": st.Enabled, "device_id": st.DeviceID, "registered": st.Registered,
		"reg_status": st.RegStatus, "registrar": st.Registrar,
		"registrar_candidates": st.RegistrarCandidates, "registrar_index": st.RegistrarIndex,
		"registrar_source": st.RegistrarSource, "last_sip_code": st.LastSIPCode,
		"last_sip_text": st.LastSIPText, "domain": st.Domain, "impi": st.IMPI, "impu": st.IMPU,
		"transport": st.Transport, "sms_receiver_transport": st.SMSReceiverTransport,
		"local_addr": st.LocalAddr, "local_port": st.LocalPort, "ipsec_installed": st.IPSecInstalled,
		"rx_running": st.RXRunning, "rx_port": st.RXPort,
		"tcp_signaling_running":    st.TCPSignalingRunning,
		"tcp_signaling_connected":  st.TCPSignalingConnected,
		"effective_security_mode":  st.EffectiveSecurityMode,
		"security_fallback_reason": st.SecurityFallbackReason,
		"security_fallback_count":  st.SecurityFallbackCount,
		"signaling_generation":     st.SignalingGeneration, "signaling_ready": st.SignalingReady,
		"signaling_failure_reason": st.SignalingFailureReason,
		"reg_fail_count":           st.RegFailCount, "re_register_pending": st.ReRegisterPending,
		"ping_fail_count": st.PingFailCount, "last_ping_at": st.LastPingAt, "last_ping_ok": st.LastPingOK,
		"last_register_trace_id":   st.LastRegisterTraceID,
		"last_register_attempt_at": st.LastRegisterAttemptAt,
		"last_register_ok_at":      st.LastRegisterOKAt, "last_register_err": st.LastRegisterErr,
		"last_sms_send_trace_id": st.LastSMSSendTraceID, "last_sms_send_at": st.LastSMSSendAt,
		"last_sms_send_err": st.LastSMSSendErr, "service_route": st.ServiceRoute, "path": st.Path,
		"security_verify": st.SecurityVerify, "associated_msisdn": st.AssociatedMSISDN,
		"last_error": st.LastError, "fragment_audit": st.FragmentAudit,
		"ims_event_bus": st.IMSEventBus, "diagnostics": st.Diagnostics,
		"state": st.State, "reg_state": st.RegState, "impus": st.IMPUs,
	}
}

// IPSec3GPPEnabled reports whether 3GPP IPsec is enabled.
func (s *Service) IPSec3GPPEnabled() bool {
	if s == nil || s.cfg == nil {
		return false
	}
	return s.cfg.IPSec3GPPEnabled()
}

// SetEnableIPSec3GPP toggles 3GPP IPsec.
func (s *Service) SetEnableIPSec3GPP(enabled bool) {
	if s == nil || s.cfg == nil {
		return
	}
	s.cfg.SetEnableIPSec3GPP(enabled)
}

// InstallIPSec3GPP installs the 3GPP IPsec policy on the network surface.
func (s *Service) InstallIPSec3GPP(policy ipsec3gpp.Policy) error {
	if s == nil || s.cfg == nil || s.cfg.IMSNetwork == nil {
		return errors.New("imscore: no network for IPsec")
	}
	installer, ok := s.cfg.IMSNetwork.(interface {
		InstallIPSec3GPP(ipsec3gpp.Policy) error
	})
	if !ok {
		return errors.New("imscore: IMS network does not support 3GPP IPsec")
	}
	return installer.InstallIPSec3GPP(policy)
}

// VoiceProfile is the voice profile of a device (recovered from the binary's
// imsendpoint.VoiceProfile).
type VoiceProfile struct {
	DeviceID string
	IMSI     string
	IMPI     string
	Domain   string
}

// VoiceProfile returns the voice profile for the device.
func (s *Service) VoiceProfile() VoiceProfile {
	if s == nil || s.cfg == nil {
		return VoiceProfile{}
	}
	return VoiceProfile{
		DeviceID: s.cfg.DeviceID,
		IMSI:     s.cfg.IMSI,
		IMPI:     s.cfg.IMPI,
		Domain:   s.cfg.Domain,
	}
}

// SendDialogRequestRaw retains the additive handle-only compatibility API.
func (s *Service) SendDialogRequestRaw(handle *imscoreDialogHandle, method string, body string) error {
	if s == nil || handle == nil {
		return errors.New("imscore: no dialog")
	}
	return errors.New("imscore: dialog target is unavailable on compatibility handle")
}

// SendReliableProvisionalPRACKRaw retains the additive handle-only compatibility API.
func (s *Service) SendReliableProvisionalPRACKRaw(handle *imscoreDialogHandle) error {
	if s == nil || handle == nil {
		return errors.New("imscore: no dialog for PRACK")
	}
	return errors.New("imscore: reliable provisional context is unavailable")
}

// StartClientInviteRaw retains the additive raw-wire client INVITE API.
func (s *Service) StartClientInviteRaw(handle *imscoreInviteHandle, invite string) error {
	if s == nil || handle == nil {
		return errors.New("imscore: client INVITE handle is required")
	}
	if strings.TrimSpace(invite) == "" || !strings.EqualFold(sipRequestMethod(invite), "INVITE") {
		return errors.New("imscore: valid INVITE request is required")
	}
	if callID := rawSIPHeaderValue(invite, "Call-ID"); callID == "" || callID != handle.id {
		return errors.New("imscore: INVITE Call-ID does not match handle")
	}
	if s.transport == nil {
		return errors.New("imscore: client INVITE SIP client is empty")
	}
	handle.mu.Lock()
	if handle.transaction != nil && !handle.done {
		handle.mu.Unlock()
		return errors.New("imscore: client INVITE transaction is already active")
	}
	handle.initialRequest = nil
	handle.done = false
	handle.confirmed = false
	handle.canceling = false
	handle.cancelSent = false
	handle.transaction = nil
	handle.mu.Unlock()
	transaction, err := s.transport.startClientTransaction(invite, sipTransactionCallbacks{})
	if err != nil {
		handle.markDone(false)
		return err
	}
	handle.mu.Lock()
	handle.initialRequest = transaction.parsed
	handle.transaction = transaction
	handle.mu.Unlock()
	go s.waitClientInviteHandle(handle, transaction)
	return nil
}

func (s *Service) waitClientInviteHandle(
	handle *imscoreInviteHandle,
	transaction *clientSIPTransaction,
) {
	response, err := s.transport.waitClientTransaction(context.Background(), transaction)
	confirmed := err == nil && response != nil && response.StatusCode >= 200 && response.StatusCode < 300
	handle.markDone(confirmed)
}

// Subscribe sends a registration event SUBSCRIBE and waits for its final response.
func (s *Service) Subscribe(uri string) error {
	if s == nil || s.cfg == nil {
		return errors.New("imscore: not configured")
	}
	uri = strings.TrimSpace(uri)
	if uri == "" || strings.ContainsAny(uri, "\r\n") {
		return errors.New("imscore: valid SUBSCRIBE URI is required")
	}
	if !strings.HasPrefix(strings.ToLower(uri), "sip:") && !strings.HasPrefix(strings.ToLower(uri), "sips:") {
		uri = "sip:" + uri
	}
	if s.transport == nil {
		return errors.New("imscore: no SIP transport")
	}
	ctx, cancel := context.WithTimeout(context.Background(), registrationSubscriptionTimeout)
	defer cancel()
	if s.hasProtectedRegistrationTransport() {
		return s.sendSubscribeReg(ctx)
	}
	publicID := primaryPublicIdentity(s.cfg)
	callID := newCallID()
	req := "SUBSCRIBE " + uri + " SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP " + formatHostPort(s.cfg.LocalIP) + ";branch=z9hG4bK" + newBranch() + "\r\n" +
		"From: <" + publicID + ">;tag=" + newTag() + "\r\n" +
		"To: <" + uri + ">\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 SUBSCRIBE\r\n" +
		"Event: reg\r\n" +
		"Expires: 3600\r\n" +
		"Content-Length: 0\r\n\r\n"
	response, err := s.transport.RoundTrip(ctx, req)
	if err != nil {
		return fmt.Errorf("imscore: SUBSCRIBE transaction: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("imscore: SUBSCRIBE rejected: %d %s", response.StatusCode, response.Reason)
	}
	return nil
}

// SMSReceiverTransport returns a snapshot of the real SIP receiver.
func (s *Service) SMSReceiverTransport() interface{} {
	return s.receiverStatus()
}

// TriggerFastReconnect triggers an immediate re-registration.
func (s *Service) TriggerFastReconnect() error {
	return s.TriggerRegisterImmediate()
}

// UpdateLastPingAt records the last keepalive ping time.
func (s *Service) UpdateLastPingAt(t time.Time) {
	if s == nil {
		return
	}
	if t.IsZero() {
		t = time.Now()
	}
	s.mu.Lock()
	if s.lastPingAt.IsZero() || t.After(s.lastPingAt) {
		s.lastPingAt = t
	}
	s.keepaliveFailures = 0
	s.mu.Unlock()
}
