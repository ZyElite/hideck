package imscore

import (
	"errors"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

func clientBuildRequest(client *sipgo.Client, request *sip.Request) error {
	if client == nil || request == nil {
		return errors.New("client/request 为空")
	}
	if request.Method == sip.REGISTER {
		return nil
	}
	return sipgo.ClientRequestBuild(client, request)
}
