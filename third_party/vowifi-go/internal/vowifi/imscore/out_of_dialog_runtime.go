package imscore

import (
	"context"
	"errors"

	"github.com/emiago/sipgo/sip"
)

func (s *Service) sendOutOfDialogRequest(
	ctx context.Context,
	modeCtx outboundModeContext,
	request *sip.Request,
) error {
	if request == nil {
		return errors.New("imscore: nil out-of-dialog request")
	}
	_, err := s.sendByMode(outboundSendOperation{
		Context: ctx,
		Mode:    modeCtx,
		Request: request,
	})
	s.handleOutboundRequestError(request, err)
	return err
}
