package imscore

import (
	"context"
	"errors"
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
		return nil
	}
	return s.respondStatus(handle.callID, 486)
}

// RespondInboundRequest responds to an inbound request with the given status.
func (s *Service) RespondInboundRequest(handle *imscoreInboundRequestHandle, status int) error {
	if s == nil || handle == nil {
		return nil
	}
	return s.respondStatus(handle.callID, status)
}

// SendDialogRequest sends a request within a dialog.
func (s *Service) SendDialogRequest(handle *imscoreDialogHandle, method string, body string) error {
	if s == nil || handle == nil {
		return errors.New("imscore: no dialog")
	}
	req := buildDialogRequest(handle, method, body, s.cfg)
	return s.sendSIP(req)
}

// SendReliableProvisionalPRACK sends a PRACK for a reliable provisional.
func (s *Service) SendReliableProvisionalPRACK(handle *imscoreDialogHandle) error {
	if s == nil || handle == nil {
		return nil
	}
	req := buildDialogRequest(handle, "PRACK", "", s.cfg)
	return s.sendSIP(req)
}

// StartClientInvite starts a client-side INVITE transaction.
func (s *Service) StartClientInvite(handle *imscoreInviteHandle, invite string) error {
	if s == nil {
		return errors.New("imscore: nil service")
	}
	return s.sendSIP(invite)
}

// Subscribe sends a SUBSCRIBE request (registration event package).
func (s *Service) Subscribe(uri string) error {
	if s == nil || s.cfg == nil {
		return errors.New("imscore: not configured")
	}
	req := "SUBSCRIBE sip:" + uri + " SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP " + formatHostPort(s.cfg.LocalIP) + ";branch=z9hG4bK" + newBranch() + "\r\n" +
		"From: <sip:" + s.cfg.IMPI + "@" + s.cfg.Domain + ">;tag=" + newTag() + "\r\n" +
		"To: <sip:" + uri + ">\r\n" +
		"Call-ID: " + newCallID() + "\r\n" +
		"CSeq: 1 SUBSCRIBE\r\n" +
		"Event: reg\r\n" +
		"Expires: 3600\r\n" +
		"Content-Length: 0\r\n\r\n"
	return s.sendSIP(req)
}

// SMSReceiverTransport returns the SMS receiver transport (nil for UDP).
func (s *Service) SMSReceiverTransport() interface{} {
	return nil
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

// respondStatus sends a synthetic status response for a call ID.
func (s *Service) respondStatus(callID string, status int) error {
	if s == nil || s.transport == nil {
		return errors.New("imscore: no transport")
	}
	resp := &sipResponse{
		StatusCode: status,
		CallID:     callID,
		Headers: map[string]string{
			"SIP/2.0": SIPStatusText(status),
		},
	}
	s.transport.DeliverResponse(resp)
	return nil
}

// buildDialogRequest builds an in-dialog request.
func buildDialogRequest(handle *imscoreDialogHandle, method, body string, cfg *IMSConfig) string {
	domain := cfg.Domain
	if domain == "" {
		domain = "ims.mnc000.mcc000.3gppnetwork.org"
	}
	impi := cfg.IMPI
	uri := "sip:" + impi
	if i := stringsIndexByte(impi, '@'); i >= 0 {
		uri = "sip:" + impi[i+1:]
	}
	fromTag := ""
	toTag := ""
	if handle != nil {
		fromTag = handle.fromTag
		toTag = handle.toTag
	}
	if fromTag == "" {
		fromTag = newTag()
	}
	contentLen := len(body)
	return "METHOD sip:" + domain + " SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP " + formatHostPort(cfg.LocalIP) + ";branch=z9hG4bK" + newBranch() + ";rport\r\n" +
		"From: <" + uri + ">;tag=" + fromTag + "\r\n" +
		"To: <" + uri + ">;tag=" + toTag + "\r\n" +
		"Call-ID: " + handle.callID + "\r\n" +
		"CSeq: 1 " + method + "\r\n" +
		"Max-Forwards: 70\r\n" +
		"Content-Length: " + itoa(contentLen) + "\r\n\r\n" + body
}

// stringsIndexByte finds a byte in a string.
func stringsIndexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// itoa converts an int to a string.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
