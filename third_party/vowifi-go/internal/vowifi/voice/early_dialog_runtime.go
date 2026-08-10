package voice

import (
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

func reliableProvisionalPRACKOptions(
	call *Call,
	rseq string,
	contact string,
	recordRoutes []string,
) imsendpoint.ReliableProvisionalOptions {
	options := imsendpoint.ReliableProvisionalOptions{
		RSeq:         strings.TrimSpace(rseq),
		Contact:      strings.TrimSpace(contact),
		RecordRoutes: append([]string(nil), recordRoutes...),
	}
	if call == nil {
		return options
	}
	options.Invite = call.IMSInviteHandleValue()
	options.Dialog = call.IMSDialogValue()
	inviteCSeq := call.voiceDialogSnapshot().inviteCSeq
	if options.RSeq != "" && inviteCSeq > 0 {
		options.RAck = fmt.Sprintf("%s %d INVITE", options.RSeq, inviteCSeq)
	}
	return options
}

func forwardedPRACKOptions(call *Call, rack string) imsendpoint.ReliableProvisionalOptions {
	options := imsendpoint.ReliableProvisionalOptions{RAck: strings.TrimSpace(rack)}
	if call == nil {
		return options
	}
	options.Invite = call.IMSInviteHandleValue()
	options.Dialog = call.IMSDialogValue()
	call.mu.RLock()
	options.Contact = strings.TrimSpace(call.DialogState.IMSContact)
	call.mu.RUnlock()
	return options
}

func (c *Call) hasLocalInviteTransaction() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.DialogState.OriginalRequest != nil && c.DialogState.ClientTx != nil
}
