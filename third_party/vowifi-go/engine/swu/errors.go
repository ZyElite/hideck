package swu

import (
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

// NegotiationError is returned when the IKEv2 SA or algorithm negotiation with
// the ePDG fails. Recovered from the decompiled (*NegotiationError).Error,
// which formats as "<context>: <reason>".
type NegotiationError struct {
	Class     string
	Reason    string
	Retryable bool

	// Context retains the interim reconstruction's field name. Class is the
	// original field and takes precedence when both are present.
	Context string
}

func (e *NegotiationError) Error() string {
	if e == nil {
		return ""
	}
	class := e.Class
	if class == "" {
		class = e.Context
	}
	return fmt.Sprintf("%s: %s", class, e.Reason)
}

// RedirectError is returned when the ePDG redirects the session to a different
// endpoint (RFC 5685). The wrapped value is the redirect target.
type RedirectError struct {
	NewAddr string
	Target  string
}

func (e *RedirectError) Error() string {
	if e == nil {
		return ""
	}
	address := e.NewAddr
	if address == "" {
		address = e.Target
	}
	return fmt.Sprintf("ePDG requested redirect to: %s", address)
}

// ErrInvalidKEGroup is returned when the ePDG requests a Diffie-Hellman group
// the client does not support (RFC 7296 INVALID_KE_PAYLOAD).
type ErrInvalidKEGroup struct {
	PreferredGroup uint16
	Group          uint16
}

func (e *ErrInvalidKEGroup) Error() string {
	if e == nil {
		return ""
	}
	group := e.PreferredGroup
	if group == 0 {
		group = e.Group
	}
	return fmt.Sprintf("服务器拒绝 DH Group，期望使用 Group %d", group)
}

// createChildSARejectError is returned when the responder rejects a CHILD_SA
// proposal; the wrapped value is the rejecting IKEv2 notify type.
type createChildSARejectError struct {
	NotifyType uint16
}

func (e *createChildSARejectError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("child SA proposal rejected: %d (%s)", e.NotifyType, notifyTypeName(e.NotifyType))
}

// notifyTypeName maps an IKEv2 notify type to its name.
func notifyTypeName(t uint16) string {
	return ikev2.NotifyTypeToString(t)
}
