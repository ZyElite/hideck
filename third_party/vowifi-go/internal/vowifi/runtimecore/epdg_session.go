package runtimecore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/epdg"
	"go.uber.org/zap"
)

const epdgEstablishmentTimeout = 45 * time.Second

func StartAndWaitEPDG(
	ctx context.Context,
	deviceID string,
	traceID string,
	config *swu.Config,
	manager *epdg.Manager,
) (*swu.Session, swu.SessionSnapshot, error) {
	if config == nil {
		return nil, swu.SessionSnapshot{}, errors.New("runtimecore: nil SWu config")
	}
	if manager == nil {
		manager = epdg.New()
	}
	session, snapshot, err := startAndWait(ctx, deviceID, config, manager)
	if epdg.ShouldRetryFreshTunnel(ctx, err) {
		zap.S().Infow("SWu first ePDG wait timed out; retrying with a fresh session",
			"device", strings.TrimSpace(deviceID), "trace_id", strings.TrimSpace(traceID))
		session, snapshot, err = startAndWait(ctx, deviceID, config, manager)
	}
	if err == nil || !shouldRetryDeviceIdentity(config, err) {
		return session, snapshot, err
	}
	zap.S().Warnw("SWu device identity rejected; retrying without spoofed identity",
		"device", strings.TrimSpace(deviceID), "trace_id", strings.TrimSpace(traceID), "error", err)
	retryConfig := *config
	retryConfig.EnableDeviceIdentitySpoof = false
	retryConfig.DeviceIdentityIMEI = ""
	return startAndWait(ctx, deviceID, &retryConfig, manager)
}

func startAndWait(
	ctx context.Context,
	deviceID string,
	config *swu.Config,
	manager *epdg.Manager,
) (*swu.Session, swu.SessionSnapshot, error) {
	session, err := manager.Start(ctx, deviceID, config)
	if err != nil {
		return nil, swu.SessionSnapshot{}, err
	}
	snapshot, err := manager.Wait(ctx, deviceID, epdgEstablishmentTimeout)
	if err == nil {
		return session, snapshot, nil
	}
	stopErr := manager.Stop(deviceID)
	if stopErr != nil && !strings.Contains(stopErr.Error(), "session id") {
		err = errors.Join(err, stopErr)
	}
	return nil, swu.SessionSnapshot{}, err
}

func shouldRetryDeviceIdentity(config *swu.Config, err error) bool {
	if config == nil || !config.EnableDeviceIdentitySpoof || err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "auth failed") ||
		strings.Contains(message, "EAP \u8ba4\u8bc1\u5931\u8d25")
}
