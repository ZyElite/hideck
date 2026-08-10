package voice

import (
	"errors"
	"fmt"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
)

const clientResponseTimeout = 2 * time.Second

func respondClientRequest(
	transaction sip.ServerTransaction,
	request *sip.Request,
	status int,
	reason string,
) error {
	response := buildClientResponseFromRequest(request, status, reason, nil)
	return respondClientTransaction(transaction, response)
}

func buildClientResponseFromRequest(
	request *sip.Request,
	status int,
	reason string,
	body []byte,
) *sip.Response {
	if request == nil {
		response := sip.NewResponse(status, reason)
		response.SetBody(body)
		return response
	}
	return sip.NewResponseFromRequest(request.Clone(), status, reason, body)
}

func respondClientTransaction(transaction sip.ServerTransaction, response *sip.Response) error {
	if transaction == nil {
		return errors.New("voice: client server transaction is unavailable")
	}
	if response == nil {
		return errors.New("voice: client response is unavailable")
	}
	return transaction.Respond(response)
}

func (a *Agent) respondClientRequestWithFallback(
	request *sip.Request,
	transaction sip.ServerTransaction,
	status int,
	reason string,
) {
	response := buildClientResponseFromRequest(request, status, reason, nil)
	if err := a.respondClientWithFallback(transaction, response); err != nil {
		logging.WarnRate(fmt.Sprintf("voice-client-response:%d", status), 10*time.Second,
			"local voice response failed", "device", a.DeviceID(), "status", status, "err", err)
	}
}

func (a *Agent) respondClientWithFallback(
	transaction sip.ServerTransaction,
	response *sip.Response,
) error {
	_, _, _, err := sipkit.DispatchResponseByVia(response, transaction, func(value *sip.Response) error {
		_, _, _, writeErr := sipkit.WriteResponseByVia(value, clientResponseTimeout)
		return writeErr
	})
	return err
}
