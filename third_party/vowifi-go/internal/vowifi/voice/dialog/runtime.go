package dialog

import (
	"context"
	"errors"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

const (
	dialogRequestDevice = "voice_dialog_request"
	prackDevice         = "voice_prack"
	closeDialogDevice   = "voice_close_dialog"
	cancelInviteDevice  = "voice_cancel_invite"
	answerInviteDevice  = "voice_answer_invite"
	rejectInviteDevice  = "voice_reject_invite"
)

var errEndpointUnavailable = errors.New("voice dialog endpoint 为空")

// SendDialogRequestWithHandle sends an ACK, BYE, UPDATE, INFO or other
// in-dialog request through the endpoint-owned route and transaction state.
func (c *Controller) SendDialogRequestWithHandle(
	ctx context.Context,
	deviceID string,
	dialog imsendpoint.DialogHandle,
	request *sip.Request,
	options imsendpoint.DialogRequestOptions,
) (*sip.Response, error) {
	if request == nil {
		return nil, errors.New("SIP request 为空")
	}
	endpoint := c.currentEndpoint()
	if endpoint == nil {
		return nil, errEndpointUnavailable
	}
	return endpoint.SendDialogRequest(ctx, operationDeviceID(deviceID, dialogRequestDevice), dialog, request, options)
}

// SendReliableProvisionalPRACK acknowledges one reliable provisional response.
func (c *Controller) SendReliableProvisionalPRACK(
	ctx context.Context,
	deviceID string,
	options imsendpoint.ReliableProvisionalOptions,
) error {
	endpoint := c.currentEndpoint()
	if endpoint == nil {
		return errEndpointUnavailable
	}
	return endpoint.SendReliableProvisionalPRACK(ctx, operationDeviceID(deviceID, prackDevice), options)
}

// CloseDialog releases endpoint-owned dialog state.
func (c *Controller) CloseDialog(
	ctx context.Context,
	deviceID string,
	dialog imsendpoint.DialogHandle,
) error {
	endpoint := c.currentEndpoint()
	if endpoint == nil {
		return errEndpointUnavailable
	}
	return endpoint.CloseDialog(ctx, operationDeviceID(deviceID, closeDialogDevice), dialog)
}

// CancelClientInvite sends the transaction-related CANCEL request.
func (c *Controller) CancelClientInvite(
	ctx context.Context,
	deviceID string,
	invite imsendpoint.InviteHandle,
	options imsendpoint.ClientInviteCancelOptions,
) error {
	endpoint := c.currentEndpoint()
	if endpoint == nil {
		return errEndpointUnavailable
	}
	return endpoint.CancelClientInvite(ctx, operationDeviceID(deviceID, cancelInviteDevice), invite, options)
}

// AnswerServerInvite sends a 2xx and returns the retained server dialog.
func (c *Controller) AnswerServerInvite(
	ctx context.Context,
	deviceID string,
	invite imsendpoint.ServerInviteHandle,
	options imsendpoint.ServerInviteAnswerOptions,
) (imsendpoint.DialogHandle, error) {
	endpoint := c.currentEndpoint()
	if endpoint == nil {
		return nil, errEndpointUnavailable
	}
	return endpoint.AnswerServerInvite(ctx, operationDeviceID(deviceID, answerInviteDevice), invite, options)
}

// RejectServerInvite sends the selected final response.
func (c *Controller) RejectServerInvite(
	ctx context.Context,
	deviceID string,
	invite imsendpoint.ServerInviteHandle,
	options imsendpoint.ServerInviteRejectOptions,
) error {
	endpoint := c.currentEndpoint()
	if endpoint == nil {
		return errEndpointUnavailable
	}
	return endpoint.RejectServerInvite(ctx, operationDeviceID(deviceID, rejectInviteDevice), invite, options)
}

func operationDeviceID(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
