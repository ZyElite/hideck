package ussi

import (
	"context"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

func sendACK(
	ctx context.Context,
	deviceID string,
	endpoint imsendpoint.ClientDialogEndpoint,
	dialog imsendpoint.DialogHandle,
	session *Session,
) {
	_, dialogContext, ok := sessionDialog(session)
	if !ok {
		logACKFailure(deviceID, "USSI 会话缺少 dialog handle")
		return
	}
	request, err := buildDialogRequest(session, sip.ACK, nil, dialogContext)
	if err != nil {
		logACKFailure(deviceID, err.Error())
		return
	}
	logUSSISIPRaw(deviceID, "ack", "send", request)
	_, err = endpoint.SendDialogRequest(
		operationContext(ctx), deviceID, dialog, request,
		imsendpoint.DialogRequestOptions{Timeout: int64(ussiTransactionTimeout)},
	)
	if err != nil {
		logACKFailure(deviceID, err.Error())
	}
}

func logACKFailure(deviceID, reason string) {
	logging.Info("IMS USSD ACK 失败", "device", deviceID, "reason", reason)
}
