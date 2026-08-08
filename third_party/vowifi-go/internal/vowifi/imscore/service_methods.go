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
func (st *ServiceStatus) ToMap() map[string]interface{} {
	if st == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"registered": st.Registered,
		"state":      st.State,
		"reg_state":  st.RegState,
		"impu":       st.IMPU,
		"domain":     st.Domain,
		"last_error": st.LastError,
	}
}

// IPSec3GPPEnabled reports whether 3GPP IPsec is enabled.
func (s *Service) IPSec3GPPEnabled() bool {
	if s == nil || s.cfg == nil {
		return false
	}
	return s.cfg.IPSec3GPPEnabled
}

// SetEnableIPSec3GPP toggles 3GPP IPsec.
func (s *Service) SetEnableIPSec3GPP(enabled bool) {
	if s == nil || s.cfg == nil {
		return
	}
	s.cfg.IPSec3GPPEnabled = enabled
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

// RejectServerInvite rejects a server-side INVITE (486 Busy Here).
func (s *Service) RejectServerInvite(handle *imscoreServerInviteHandle) error {
	if s == nil || handle == nil {
		return errors.New("imscore: server INVITE handle is required")
	}
	return errors.New("imscore: cannot reject INVITE without its inbound request context")
}

// RespondInboundRequest responds to an inbound request with the given status.
func (s *Service) RespondInboundRequest(handle *imscoreInboundRequestHandle, status int) error {
	if s == nil || handle == nil {
		return errors.New("imscore: inbound request handle is required")
	}
	return errors.New("imscore: cannot respond without the inbound request context")
}

// SendDialogRequest sends a request within a dialog.
func (s *Service) SendDialogRequest(handle *imscoreDialogHandle, method string, body string) error {
	if s == nil || handle == nil {
		return errors.New("imscore: no dialog")
	}
	return errors.New("imscore: dialog target is unavailable on compatibility handle")
}

// SendReliableProvisionalPRACK sends a PRACK for a reliable provisional.
func (s *Service) SendReliableProvisionalPRACK(handle *imscoreDialogHandle) error {
	if s == nil || handle == nil {
		return errors.New("imscore: no dialog for PRACK")
	}
	return errors.New("imscore: reliable provisional context is unavailable")
}

// StartClientInvite starts a client-side INVITE transaction.
func (s *Service) StartClientInvite(handle *imscoreInviteHandle, invite string) error {
	if s == nil || handle == nil {
		return errors.New("imscore: client INVITE handle is required")
	}
	if strings.TrimSpace(invite) == "" || !strings.EqualFold(sipRequestMethod(invite), "INVITE") {
		return errors.New("imscore: valid INVITE request is required")
	}
	if callID := rawSIPHeaderValue(invite, "Call-ID"); callID == "" || callID != handle.callID {
		return errors.New("imscore: INVITE Call-ID does not match handle")
	}
	return s.sendSIP(invite)
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
	s.mu.Lock()
	s.lastPingAt = t
	s.mu.Unlock()
}
