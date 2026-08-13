package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/automation"
	"github.com/iniwex5/vohive/internal/db"
	"github.com/iniwex5/vohive/internal/device"
	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

const automationPollInterval = 500 * time.Millisecond

type automaticTaskExecutor struct{ server *Server }

func (e *automaticTaskExecutor) Execute(
	ctx context.Context,
	task automation.Task,
	progress func(string) error,
) (output string, executeErr error) {
	if e == nil || e.server == nil || e.server.pool == nil {
		return "", errors.New("device pool is unavailable")
	}
	worker := e.server.pool.GetWorker(task.DeviceID)
	if worker == nil {
		return "", fmt.Errorf("device %s is offline", task.DeviceID)
	}
	if err := reportAutomationProgress(progress, "正在切换目标 eSIM profile"); err != nil {
		return "", err
	}
	if err := e.server.pool.SwitchESIMProfileAndWait(ctx, task.DeviceID, task.ProfileICCID, task.ProfileAID); err != nil {
		return "", err
	}

	snapshot, err := db.ResolveCardPolicy(task.ProfileICCID)
	if err != nil {
		return "", fmt.Errorf("resolve card policy: %w", err)
	}
	actionStarted := false
	defer func() {
		restoreErr := e.restoreEnvironment(task.DeviceID, snapshot)
		if restoreErr != nil {
			executeErr = errors.Join(executeErr, fmt.Errorf("restore card policy: %w", restoreErr))
			if actionStarted && (task.TaskType == automation.TaskTypeSMS || task.TaskType == automation.TaskTypeCall) {
				executeErr = automation.Permanent(executeErr)
			}
		}
	}()
	if err := e.prepareEnvironment(ctx, task, snapshot, progress); err != nil {
		return "", err
	}

	worker = e.server.pool.GetWorker(task.DeviceID)
	if worker == nil {
		return "", fmt.Errorf("device %s went offline", task.DeviceID)
	}
	actionStarted = true
	switch task.TaskType {
	case automation.TaskTypeSMS:
		return e.executeSMS(ctx, worker, task, progress)
	case automation.TaskTypeCall:
		return e.executeCall(ctx, worker, task, progress)
	case automation.TaskTypePublicIP:
		return executePublicIP(worker, progress)
	default:
		return "", fmt.Errorf("unsupported automatic task type %q", task.TaskType)
	}
}

func (e *automaticTaskExecutor) prepareEnvironment(
	ctx context.Context,
	task automation.Task,
	snapshot db.CardPolicy,
	progress func(string) error,
) error {
	policy := snapshot
	policy.Source = "automatic_task"
	policy.AirplaneEnabled = false
	policy.VoWiFiEnabled = task.Environment == automation.EnvironmentVoWiFi
	policy.NetworkEnabled = task.TaskType == automation.TaskTypePublicIP
	if err := db.UpsertCardPolicy(policy); err != nil {
		return fmt.Errorf("persist automatic task card policy: %w", err)
	}
	if err := reportAutomationProgress(progress, "正在准备 "+task.Environment+" 运行环境"); err != nil {
		return err
	}
	if task.Environment == automation.EnvironmentVoWiFi {
		return e.prepareVoWiFi(ctx, task)
	}
	return e.prepareCellular(task)
}

func (e *automaticTaskExecutor) prepareVoWiFi(ctx context.Context, task automation.Task) error {
	if err := e.server.pool.ApplyCurrentCardPolicy(task.DeviceID, "automatic_task_prepare_vowifi"); err != nil {
		return err
	}
	if !e.server.pool.IsVoWiFiActive(task.DeviceID) {
		if err := e.server.pool.EnableVoWiFi(task.DeviceID); err != nil {
			return fmt.Errorf("enable VoWiFi: %w", err)
		}
	}
	requireSMS := task.TaskType == automation.TaskTypeSMS
	return waitForVoWiFiReady(ctx, e.server.pool, task.DeviceID, requireSMS)
}

func (e *automaticTaskExecutor) prepareCellular(task automation.Task) error {
	if e.server.pool.IsVoWiFiActive(task.DeviceID) {
		if err := e.server.pool.DisableVoWiFi(task.DeviceID); err != nil {
			return fmt.Errorf("disable VoWiFi: %w", err)
		}
	}
	if err := e.server.pool.ApplyCurrentCardPolicy(task.DeviceID, "automatic_task_prepare_cellular"); err != nil {
		return err
	}
	worker := e.server.pool.GetWorker(task.DeviceID)
	if worker == nil || !strings.EqualFold(worker.Config.DeviceBackend, "pcsc") {
		return nil
	}
	return errors.New("PCSC reader devices do not provide a cellular environment")
}

func (e *automaticTaskExecutor) restoreEnvironment(deviceID string, snapshot db.CardPolicy) error {
	if err := db.UpsertCardPolicy(snapshot); err != nil {
		return err
	}
	if !snapshot.VoWiFiEnabled && e.server.pool.IsVoWiFiActive(deviceID) {
		if err := e.server.pool.DisableVoWiFi(deviceID); err != nil {
			return err
		}
	}
	if err := e.server.pool.ApplyCurrentCardPolicy(deviceID, "automatic_task_restore"); err != nil {
		return err
	}
	if snapshot.VoWiFiEnabled && !e.server.pool.IsVoWiFiActive(deviceID) {
		if err := e.server.pool.EnableVoWiFi(deviceID); err != nil {
			return err
		}
	}
	if snapshot.VoWiFiEnabled {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return waitForVoWiFiReady(ctx, e.server.pool, deviceID, false)
	}
	return nil
}

func (e *automaticTaskExecutor) executeSMS(
	ctx context.Context,
	worker *device.Worker,
	task automation.Task,
	progress func(string) error,
) (string, error) {
	if err := reportAutomationProgress(progress, "正在提交短信"); err != nil {
		return "", err
	}
	if task.Environment == automation.EnvironmentVoWiFi {
		outcome, err := e.server.pool.SendVoWiFiSMSWithResult(ctx, task.DeviceID, task.Payload.Phone, task.Payload.Message)
		if err != nil {
			return "", automation.Permanent(fmt.Errorf("send VoWiFi SMS: %w", err))
		}
		return fmt.Sprintf("短信已提交，message_id=%s，delivery_state=%s，parts=%d", outcome.MessageID, outcome.DeliveryState, outcome.PartsTotal), nil
	}
	if err := worker.SendSMS(task.Payload.Phone, task.Payload.Message); err != nil {
		return "", automation.Permanent(fmt.Errorf("send cellular SMS: %w", err))
	}
	return "蜂窝短信已由模组提交", nil
}

func (e *automaticTaskExecutor) executeCall(
	ctx context.Context,
	worker *device.Worker,
	task automation.Task,
	progress func(string) error,
) (string, error) {
	if err := reportAutomationProgress(progress, "正在发起通话"); err != nil {
		return "", err
	}
	if task.Environment == automation.EnvironmentVoWiFi {
		if e.server.voiceGW == nil {
			return "", errors.New("VoWiFi voice gateway is unavailable")
		}
		result, err := e.server.voiceGW.SimulateCall(ctx, task.DeviceID, voicehost.SimulateCallRequest{
			Callee: task.Payload.Phone, HoldSeconds: task.Payload.HoldSeconds,
		})
		if err != nil {
			return "", automation.Permanent(fmt.Errorf("place VoWiFi call: %w", err))
		}
		if result == nil || !result.Success {
			return "", automation.Permanent(errors.New("VoWiFi call did not complete successfully"))
		}
		return fmt.Sprintf("VoWiFi 通话已完成，duration_ms=%d", result.DurationMs), nil
	}
	if worker.Modem == nil {
		return "", errors.New("cellular calling requires an AT modem channel")
	}
	if err := worker.Modem.DialCall(task.Payload.Phone); err != nil {
		return "", automation.Permanent(err)
	}
	timer := time.NewTimer(time.Duration(task.Payload.HoldSeconds) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		_ = worker.Modem.HangupCall()
		return "", automation.Permanent(ctx.Err())
	case <-timer.C:
	}
	if err := worker.Modem.HangupCall(); err != nil {
		return "", automation.Permanent(err)
	}
	return "蜂窝通话已拨号并按计划挂机", nil
}

func executePublicIP(worker *device.Worker, progress func(string) error) (string, error) {
	if err := reportAutomationProgress(progress, "正在查询公网 IP"); err != nil {
		return "", err
	}
	controller := worker.NetworkController()
	if controller == nil {
		return "", errors.New("device has no cellular network controller")
	}
	if !controller.IsConnected() {
		if err := worker.StartNetwork(); err != nil {
			return "", fmt.Errorf("start cellular network: %w", err)
		}
	}
	publicV4, publicV6 := controller.GetPublicIPv4AndV6NoCache()
	if strings.TrimSpace(publicV4) == "" && strings.TrimSpace(publicV6) == "" {
		return "", errors.New("public IP query returned no address")
	}
	return fmt.Sprintf("public_ipv4=%s public_ipv6=%s", publicV4, publicV6), nil
}

func waitForVoWiFiReady(ctx context.Context, pool *device.Pool, deviceID string, requireSMS bool) error {
	ticker := time.NewTicker(automationPollInterval)
	defer ticker.Stop()
	for {
		state, ok := pool.GetVoWiFiRuntimeState(deviceID)
		if ok && state.IMSReady && (!requireSMS || state.SMSReady) {
			return nil
		}
		if ok && state.LastError != "" && (state.IMSState == "failed" || state.SessionState == "error") {
			return fmt.Errorf("VoWiFi startup failed: %s", state.LastError)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for VoWiFi readiness: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func reportAutomationProgress(progress func(string) error, value string) error {
	if progress == nil {
		return nil
	}
	return progress(value)
}
