package device

import (
	"context"
	"errors"
	"time"

	"github.com/yibaiba/hideck/internal/backend"
	"github.com/yibaiba/hideck/internal/pcsc"
	"github.com/yibaiba/hideck/pkg/smscodec"
	swusim "github.com/iniwex5/vowifi-go/engine/sim"
)

const pcscAKAOperationTimeout = 15 * time.Second

func (*pcscDeviceBackend) SetOperatingMode(context.Context, backend.OperatingMode) error {
	return errPCSCOperationUnsupported
}

func (*pcscDeviceBackend) GetOperatingMode(context.Context) (backend.OperatingMode, error) {
	return backend.ModeRFOff, nil
}

func (*pcscDeviceBackend) Reboot(context.Context) error { return errPCSCOperationUnsupported }

func (*pcscDeviceBackend) SendSMS(context.Context, string, string) error {
	return errPCSCOperationUnsupported
}

func (*pcscDeviceBackend) SendSMSWithOptions(context.Context, string, string, smscodec.SubmitOptions) error {
	return errPCSCOperationUnsupported
}

func (*pcscDeviceBackend) ReadSMS(context.Context, int) (*backend.SMS, error) {
	return nil, errPCSCOperationUnsupported
}

func (*pcscDeviceBackend) DeleteSMS(context.Context, int) error {
	return errPCSCOperationUnsupported
}

func (*pcscDeviceBackend) ListSMS(context.Context) ([]backend.SMSSummary, error) {
	return nil, errPCSCOperationUnsupported
}

func (*pcscDeviceBackend) DeleteAllSMS(context.Context) error {
	return errPCSCOperationUnsupported
}

type pcscAKAProvider struct {
	backend *pcscDeviceBackend
	ICCID   string
}

func (provider *pcscAKAProvider) CalculateAKA(rand16, autn16 []byte) (swusim.AKAResult, error) {
	if len(rand16) != 16 || len(autn16) != 16 {
		return swusim.AKAResult{}, errors.New("pcsc: AKA RAND and AUTN must be 16 bytes")
	}
	var challenge pcsc.AKAChallenge
	copy(challenge.RAND[:], rand16)
	copy(challenge.AUTN[:], autn16)
	pin, err := provider.backend.simPIN()
	if err != nil {
		return swusim.AKAResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pcscAKAOperationTimeout)
	defer cancel()
	result, err := provider.backend.service.Authenticate(
		ctx, provider.backend.selector, provider.ICCID, pin, challenge,
	)
	if err != nil {
		return swusim.AKAResult{}, err
	}
	converted := swusim.AKAResult{
		RES: result.RES, CK: result.CK, IK: result.IK, AUTS: result.AUTS,
	}
	if result.SynchronizationFailure {
		return converted, swusim.ErrSyncFailure
	}
	return converted, nil
}
