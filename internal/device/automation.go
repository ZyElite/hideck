package device

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const automationStatePollInterval = 500 * time.Millisecond

// ApplyCurrentCardPolicy reapplies the persisted policy for the device's active ICCID.
func (p *Pool) ApplyCurrentCardPolicy(deviceID, reason string) error {
	if p == nil {
		return errors.New("device pool is unavailable")
	}
	worker := p.GetWorker(strings.TrimSpace(deviceID))
	if worker == nil {
		return fmt.Errorf("device %s is unavailable", deviceID)
	}
	result := p.resolveAndApplyPolicy(worker, strings.TrimSpace(reason))
	if !result.Applied {
		if result.Reason == "" {
			result.Reason = "policy resolver is unavailable"
		}
		return fmt.Errorf("apply card policy for device %s: %s", deviceID, result.Reason)
	}
	return nil
}

// SwitchESIMProfileAndWait waits for both the asynchronous recovery flow and
// the runtime ICCID projection, not only the initial EnableProfile response.
func (p *Pool) SwitchESIMProfileAndWait(ctx context.Context, deviceID, iccid, aid string) error {
	if p == nil {
		return errors.New("device pool is unavailable")
	}
	target := normalizeAutomationICCID(iccid)
	if target == "" {
		return errors.New("target ICCID is empty")
	}
	worker := p.GetWorker(strings.TrimSpace(deviceID))
	if worker == nil {
		return fmt.Errorf("device %s is unavailable", deviceID)
	}
	if normalizeAutomationICCID(worker.CurrentICCID()) == target && !p.IsESIMSwitching(deviceID) {
		return nil
	}
	if worker.EsimMgr == nil {
		return fmt.Errorf("device %s has no eSIM manager", deviceID)
	}
	if _, err := worker.EsimMgr.SwitchProfileWithResult(ctx, target, strings.TrimSpace(aid)); err != nil {
		return fmt.Errorf("switch device %s to profile %s: %w", deviceID, target, err)
	}

	ticker := time.NewTicker(automationStatePollInterval)
	defer ticker.Stop()
	for {
		current := p.GetWorker(deviceID)
		if current != nil && !p.IsESIMSwitching(deviceID) && normalizeAutomationICCID(current.CurrentICCID()) == target {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for device %s profile %s: %w", deviceID, target, ctx.Err())
		case <-ticker.C:
		}
	}
}

func normalizeAutomationICCID(value string) string {
	return strings.TrimRight(strings.Trim(strings.TrimSpace(value), "\""), "Ff")
}
