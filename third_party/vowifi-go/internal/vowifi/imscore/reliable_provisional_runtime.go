package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
)

const reliableProvisionalTimeout = 5 * time.Second

// SendReliableProvisionalPRACK builds and sends the PRACK for a reliable 1xx.
func (s *Service) SendReliableProvisionalPRACK(
	ctx context.Context,
	_ string,
	options imsendpoint.ReliableProvisionalOptions,
) error {
	if s == nil || !s.IsRegistered() {
		return errors.New("reliable provisional PRACK requires registered IMS")
	}
	invite, dialog, err := s.reliableProvisionalRuntime(options)
	if err != nil {
		return err
	}
	request, err := buildReliableProvisionalPRACKRequest(options, invite, dialog)
	if err != nil {
		return err
	}
	_, err = s.sendDialogRequestByMode(ctx, dialog, request, reliableProvisionalTimeout)
	return err
}

// SendReliableProvisionalPRACKForCurrentDevice retains the device-implicit API.
func (s *Service) SendReliableProvisionalPRACKForCurrentDevice(
	ctx context.Context,
	options imsendpoint.ReliableProvisionalOptions,
) error {
	return s.SendReliableProvisionalPRACK(ctx, s.DeviceID(), options)
}

func (s *Service) reliableProvisionalRuntime(
	options imsendpoint.ReliableProvisionalOptions,
) (*imscoreInviteHandle, *imscoreDialogHandle, error) {
	invite, err := concreteReliableInvite(options.Invite)
	if err != nil {
		return nil, nil, err
	}
	dialog, err := concreteReliableDialog(options.Dialog)
	if err != nil {
		return nil, nil, err
	}
	if dialog == nil && invite != nil {
		invite.mu.Lock()
		session := invite.dialog
		invite.mu.Unlock()
		if session != nil {
			dialog = s.dialogs().load(session.ID)
		}
	}
	if dialog == nil {
		return nil, nil, errors.New("reliable provisional dialog is unavailable")
	}
	applyReliableProvisionalRouteSet(dialog, options.RecordRoutes)
	return invite, dialog, nil
}

func concreteReliableInvite(value imsendpoint.InviteHandle) (*imscoreInviteHandle, error) {
	if value == nil {
		return nil, nil
	}
	invite, ok := value.(*imscoreInviteHandle)
	if !ok || invite == nil {
		return nil, errors.New("reliable provisional INVITE handle 类型无效")
	}
	return invite, nil
}

func concreteReliableDialog(value imsendpoint.DialogHandle) (*imscoreDialogHandle, error) {
	if value == nil {
		return nil, nil
	}
	dialog, ok := value.(*imscoreDialogHandle)
	if !ok || dialog == nil {
		return nil, errors.New("reliable provisional dialog handle 类型无效")
	}
	return dialog, nil
}

func applyReliableProvisionalRouteSet(dialog *imscoreDialogHandle, routes []string) {
	if dialog == nil || len(routes) == 0 {
		return
	}
	cleaned := make([]string, 0, len(routes))
	for index := len(routes) - 1; index >= 0; index-- {
		if route := strings.TrimSpace(routes[index]); route != "" {
			cleaned = append(cleaned, route)
		}
	}
	if len(cleaned) == 0 {
		return
	}
	dialog.mu.Lock()
	dialog.routeSet = cleaned
	dialog.mu.Unlock()
}

func buildReliableProvisionalPRACKRequest(
	options imsendpoint.ReliableProvisionalOptions,
	invite *imscoreInviteHandle,
	dialog *imscoreDialogHandle,
) (*sip.Request, error) {
	rack := reliableProvisionalRAck(options, invite, dialog)
	if rack == "" {
		return nil, errors.New("reliable provisional RAck is empty")
	}
	recipient, err := reliableProvisionalRecipient(options.Contact, dialog)
	if err != nil {
		return nil, err
	}
	return sipkit.BuildMinimalDialogRequest(sip.PRACK, recipient, sipkit.DialogRequestOptions{
		Headers: []sip.Header{sip.NewHeader("RAck", rack)},
	})
}

func reliableProvisionalRAck(
	options imsendpoint.ReliableProvisionalOptions,
	invite *imscoreInviteHandle,
	dialog *imscoreDialogHandle,
) string {
	if rack := strings.TrimSpace(options.RAck); rack != "" {
		return rack
	}
	rseq := strings.TrimSpace(options.RSeq)
	request := reliableProvisionalInviteRequest(invite, dialog)
	if rseq == "" || request == nil || request.CSeq() == nil {
		return ""
	}
	cseq := request.CSeq()
	return fmt.Sprintf("%s %d %s", rseq, cseq.SeqNo, cseq.MethodName)
}

func reliableProvisionalInviteRequest(
	invite *imscoreInviteHandle,
	dialog *imscoreDialogHandle,
) *sip.Request {
	if invite != nil {
		invite.mu.Lock()
		request := invite.initialRequest
		invite.mu.Unlock()
		if request != nil {
			return request
		}
	}
	if dialog != nil {
		return dialog.inviteRequest
	}
	return nil
}

func reliableProvisionalRecipient(contact string, dialog *imscoreDialogHandle) (sip.Uri, error) {
	if recipient, ok := parseReliableProvisionalContactURI(contact); ok {
		return recipient, nil
	}
	if dialog == nil {
		return sip.Uri{}, errors.New("reliable provisional recipient is unavailable")
	}
	dialog.mu.Lock()
	recipient := *dialog.remoteTarget.Clone()
	dialog.mu.Unlock()
	if recipient.Host == "" {
		return sip.Uri{}, errors.New("reliable provisional recipient is unavailable")
	}
	return recipient, nil
}

func parseReliableProvisionalContactURI(value string) (sip.Uri, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return sip.Uri{}, false
	}
	var uri sip.Uri
	params := sip.NewParams()
	if _, err := sip.ParseAddressValue(value, &uri, &params); err == nil && uri.Host != "" {
		return uri, true
	}
	if err := sip.ParseUri(strings.Trim(value, "<>"), &uri); err != nil || uri.Host == "" {
		return sip.Uri{}, false
	}
	return uri, true
}
