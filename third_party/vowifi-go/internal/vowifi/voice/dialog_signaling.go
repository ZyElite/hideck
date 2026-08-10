package voice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

func (a *Agent) startVoiceClientInvite(
	ctx context.Context,
	call *Call,
	raw string,
) (imscore.SIPResponse, error) {
	request, err := parseVoiceRequest(raw)
	if err != nil {
		return imscore.SIPResponse{}, fmt.Errorf("voice: parse INVITE: %w", err)
	}
	var finalObserved atomic.Bool
	result, err := a.ims.StartClientInvite(ctx, a.deviceID, imsendpoint.ClientInviteOptions{
		Request: request,
		Contact: request.Contact(),
		OnStarted: func(handle imsendpoint.InviteHandle) error {
			return call.storeInviteHandle(handle)
		},
		OnResponse: func(response *sip.Response) error {
			value := publicVoiceSIPResponse(response)
			if value.StatusCode < 200 {
				return a.handleIMSResponseCallback(ctx, call, response)
			}
			updateCallDialogFromResponse(call, response)
			if !finalObserved.Swap(true) {
				return nil
			}
			return a.ackRetransmittedInvite(call, value)
		},
	})
	if result != nil {
		if storeErr := call.storeInviteHandle(result.InviteHandle); storeErr != nil {
			err = errors.Join(err, storeErr)
		}
		if storeErr := call.storeDialogHandle(result.Dialog); storeErr != nil {
			err = errors.Join(err, storeErr)
		}
	}
	if result == nil || result.Response == nil {
		return imscore.SIPResponse{}, err
	}
	return publicVoiceSIPResponse(result.Response), err
}

func (a *Agent) ackRetransmittedInvite(call *Call, response imscore.SIPResponse) error {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil
	}
	call.learnVoiceDialog(response)
	request := buildIMSACKForStatus(a, call, response.StatusCode)
	if _, err := a.sendCallDialogRequest(context.Background(), call, request); err != nil {
		return fmt.Errorf("voice: resend INVITE ACK: %w", err)
	}
	call.MarkACKSent()
	return nil
}

func (c *Call) storeInviteHandle(handle imsendpoint.InviteHandle) error {
	if handle == nil {
		return nil
	}
	concrete, ok := handle.(*imscore.InviteHandle)
	if !ok || concrete == nil {
		return errors.New("voice: IMS INVITE handle type is invalid")
	}
	c.SetIMSInviteHandle(concrete)
	return nil
}

func (c *Call) storeDialogHandle(handle imsendpoint.DialogHandle) error {
	if handle == nil {
		return nil
	}
	concrete, ok := handle.(*imscore.DialogHandle)
	if !ok || concrete == nil {
		return errors.New("voice: IMS dialog handle type is invalid")
	}
	c.SetIMSDialog(concrete)
	return nil
}

func (a *Agent) sendCallDialogRequest(
	ctx context.Context,
	call *Call,
	raw string,
) (imscore.SIPResponse, error) {
	if a == nil || a.dialog == nil || call == nil {
		return imscore.SIPResponse{}, errors.New("voice: dialog controller is unavailable")
	}
	handle := call.IMSDialog()
	if handle == nil {
		return imscore.SIPResponse{}, errors.New("voice: IMS dialog handle is unavailable")
	}
	request, err := parseVoiceRequest(raw)
	if err != nil {
		return imscore.SIPResponse{}, err
	}
	response, err := a.dialog.SendDialogRequestWithHandle(
		ctx, a.deviceID, handle, request, imsendpoint.DialogRequestOptions{},
	)
	if err != nil {
		return imscore.SIPResponse{}, err
	}
	if response == nil {
		return imscore.SIPResponse{StatusCode: 200, Reason: "OK"}, nil
	}
	return publicVoiceSIPResponse(response), nil
}

func (a *Agent) cancelVoiceClientInvite(ctx context.Context, call *Call, reason string) error {
	if a == nil || a.dialog == nil || call == nil {
		return errors.New("voice: dialog controller is unavailable")
	}
	handle := call.IMSInviteHandle()
	if handle == nil {
		return errors.New("voice: IMS INVITE handle is unavailable")
	}
	call.MarkLocalCancelSent(reason)
	return a.dialog.CancelClientInvite(ctx, a.deviceID, handle, imsendpoint.ClientInviteCancelOptions{
		Reason: strings.TrimSpace(reason),
	})
}

func (a *Agent) closeLateAcceptedInvite(
	ctx context.Context,
	call *Call,
	response imscore.SIPResponse,
) error {
	call.learnVoiceDialog(response)
	ack := buildIMSACKForStatus(a, call, response.StatusCode)
	if _, err := a.sendCallDialogRequest(ctx, call, ack); err != nil {
		closeErr := a.closeCallDialog(context.Background(), call)
		return errors.Join(fmt.Errorf("voice: ACK late accepted INVITE: %w", err), closeErr)
	}
	call.MarkACKSent()
	byeResponse, err := a.sendCallDialogRequest(ctx, call, BuildIMSBye(a, call))
	if err != nil {
		closeErr := a.closeCallDialog(context.Background(), call)
		return errors.Join(fmt.Errorf("voice: close late accepted INVITE: %w", err), closeErr)
	}
	if byeResponse.StatusCode < 200 || byeResponse.StatusCode >= 300 {
		closeErr := a.closeCallDialog(context.Background(), call)
		return errors.Join(fmt.Errorf(
			"voice: late accepted INVITE BYE rejected: %d %s",
			byeResponse.StatusCode, byeResponse.Reason,
		), closeErr)
	}
	if err := a.closeCallDialog(ctx, call); err != nil {
		return fmt.Errorf("voice: release late accepted dialog: %w", err)
	}
	return errors.New("voice: INVITE accepted after local CANCEL; dialog acknowledged and closed")
}

func (a *Agent) closeCallDialog(ctx context.Context, call *Call) error {
	if a == nil || a.dialog == nil || call == nil {
		return errors.New("voice: dialog controller is unavailable")
	}
	handle := call.IMSDialog()
	if handle == nil {
		return nil
	}
	if err := a.dialog.CloseDialog(ctx, a.deviceID, handle); err != nil {
		return err
	}
	call.SetIMSDialog(nil)
	return nil
}

func parseVoiceRequest(raw string) (*sip.Request, error) {
	message, err := sip.ParseMessage([]byte(raw))
	if err != nil {
		return nil, err
	}
	request, ok := message.(*sip.Request)
	if !ok {
		return nil, errors.New("voice: SIP message is not a request")
	}
	return request, nil
}

func publicVoiceSIPResponse(response *sip.Response) imscore.SIPResponse {
	if response == nil {
		return imscore.SIPResponse{}
	}
	result := imscore.SIPResponse{
		StatusCode: response.StatusCode,
		Reason:     response.Reason,
		Headers:    make(map[string]string),
		Body:       append([]byte(nil), response.Body()...),
	}
	for _, header := range response.Headers() {
		value := header.Value()
		if previous := result.Headers[header.Name()]; previous != "" {
			value = previous + ", " + value
		}
		result.Headers[header.Name()] = value
	}
	return result
}
