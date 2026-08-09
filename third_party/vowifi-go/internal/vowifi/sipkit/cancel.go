package sipkit

import (
	"errors"

	"github.com/emiago/sipgo/sip"
)

// BuildCancelFromInvite creates a CANCEL that shares the INVITE transaction.
func BuildCancelFromInvite(invite *sip.Request) (*sip.Request, error) {
	if err := validateInviteForCancel(invite); err != nil {
		return nil, err
	}
	cancel := sip.NewRequest(sip.CANCEL, *invite.Recipient.Clone())
	cancel.SipVersion = invite.SipVersion
	cancel.AppendHeader(invite.Via().Clone())
	for _, route := range invite.GetHeaders("Route") {
		cancel.AppendHeader(sip.HeaderClone(route))
	}
	maxForwards := sip.MaxForwardsHeader(70)
	cancel.AppendHeader(&maxForwards)
	cancel.AppendHeader(sip.HeaderClone(invite.From()))
	cancel.AppendHeader(sip.HeaderClone(invite.To()))
	cancel.AppendHeader(sip.HeaderClone(invite.CallID()))
	cancel.AppendHeader(sip.HeaderClone(invite.CSeq()))
	cancel.CSeq().MethodName = sip.CANCEL
	cancel.SetTransport(invite.Transport())
	cancel.SetSource(invite.Source())
	cancel.SetDestination(invite.Destination())
	cancel.SetBody(nil)
	return cancel, nil
}

func validateInviteForCancel(invite *sip.Request) error {
	if invite == nil {
		return errors.New("INVITE request is nil")
	}
	if invite.Via() == nil {
		return errors.New("INVITE Via is nil")
	}
	if invite.From() == nil {
		return errors.New("INVITE From is nil")
	}
	if invite.To() == nil {
		return errors.New("INVITE To is nil")
	}
	if invite.CallID() == nil {
		return errors.New("INVITE Call-ID is nil")
	}
	if invite.CSeq() == nil {
		return errors.New("INVITE CSeq is nil")
	}
	return nil
}
