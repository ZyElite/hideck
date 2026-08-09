package sipkit

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
)

const (
	responseModeTransaction  = "transaction"
	responseModeStatelessVia = "stateless_via"
)

// DispatchResponseByVia prefers the server transaction and falls back to the
// top Via route when the transaction is absent or fails.
func DispatchResponseByVia(
	response *sip.Response,
	transaction sip.ServerTransaction,
	statelessWriter func(*sip.Response) error,
) (mode, transport, destination string, err error) {
	if response == nil {
		return "", "", "", errors.New("SIP response 为空")
	}
	var transactionErr error
	if transaction != nil {
		transactionErr = transaction.Respond(response)
		if transactionErr == nil {
			return responseModeTransaction, "", "", nil
		}
	}
	transport, destination, _, routeErr := resolveResponseVia(response)
	if routeErr != nil {
		return responseModeStatelessVia, "", "", combineDispatchErrors(transactionErr, "via route resolve failed", routeErr)
	}
	response.SetTransport(transport)
	response.SetDestination(destination)
	if statelessWriter == nil {
		return responseModeStatelessVia, transport, destination, combineDispatchErrors(
			transactionErr, "stateless response writer failed", errors.New("stateless response writer 为空"),
		)
	}
	if writeErr := statelessWriter(response); writeErr != nil {
		return responseModeStatelessVia, transport, destination, combineDispatchErrors(transactionErr, "stateless response writer failed", writeErr)
	}
	return responseModeStatelessVia, transport, destination, nil
}

func combineDispatchErrors(transactionErr error, operation string, fallbackErr error) error {
	if transactionErr == nil {
		return fallbackErr
	}
	return fmt.Errorf("tx respond failed: %v; %s: %w", transactionErr, operation, fallbackErr)
}

func resolveResponseVia(response *sip.Response) (transport, destination, source string, err error) {
	if response.Via() == nil {
		return "", "", "", errors.New("SIP response 缺少 Via 头")
	}
	return ResolveViaRoute(response.Via())
}

// WriteResponseByVia writes a response directly to the route from its top Via.
func WriteResponseByVia(
	response *sip.Response,
	timeout time.Duration,
) (transport, destination, source string, err error) {
	if response == nil {
		return "", "", "", errors.New("SIP response 为空")
	}
	transport, destination, source, err = resolveResponseVia(response)
	if err != nil {
		return transport, destination, source, err
	}
	response.SetTransport(transport)
	response.SetDestination(destination)
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(context.Background(), strings.ToLower(transport), destination)
	if err != nil {
		return transport, destination, source, err
	}
	defer connection.Close()
	if timeout > 0 {
		if err := connection.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
			return transport, destination, source, err
		}
	}
	_, err = connection.Write([]byte(response.String()))
	return transport, destination, source, err
}
