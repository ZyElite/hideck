package automation

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (s *Service) scheduleLoop(recoveryDone <-chan struct{}) {
	defer s.wg.Done()
	select {
	case <-recoveryDone:
	case <-s.ctx.Done():
		return
	}
	s.claimDue()
	ticker := time.NewTicker(s.options.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.claimDue()
		}
	}
}

func (s *Service) claimDue() {
	runs, err := s.store.ClaimDueRuns(s.ctx, s.options.Now().UTC(), deviceQueueSize)
	if err != nil {
		s.report(fmt.Errorf("claim automatic task runs: %w", err))
		return
	}
	for _, run := range runs {
		if err := s.enqueue(run); err != nil {
			s.report(err)
			return
		}
	}
}

func (s *Service) enqueue(run Run) error {
	s.mu.Lock()
	if !s.started || s.ctx == nil {
		s.mu.Unlock()
		return ErrNotStarted
	}
	queue := s.queues[run.DeviceID]
	if queue == nil {
		queue = make(chan Run, deviceQueueSize)
		s.queues[run.DeviceID] = queue
		s.wg.Add(1)
		go s.deviceLoop(queue)
	}
	ctx := s.ctx
	s.mu.Unlock()
	select {
	case queue <- run:
		return nil
	case <-ctx.Done():
		return ErrNotStarted
	}
}

func (s *Service) deviceLoop(queue <-chan Run) {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case run := <-queue:
			if s.ctx.Err() != nil {
				return
			}
			s.execute(run)
		}
	}
}

func (s *Service) execute(run Run) {
	task, err := s.store.GetTask(s.ctx, run.TaskID)
	if err != nil {
		s.finishWithError(run, err)
		return
	}
	startedAt := s.options.Now().UTC()
	run.StartedAt = &startedAt
	run.Status = RunStatusRunning
	if err := s.store.UpdateRun(s.ctx, run); err != nil {
		s.report(fmt.Errorf("mark automatic task run %d running: %w", run.ID, err))
		return
	}

	output, executeErr := s.executeAttempts(task, &run)
	run.Output = output
	s.finish(task, run, executeErr)
}

func (s *Service) executeAttempts(task Task, run *Run) (string, error) {
	var output string
	var executeErr error
	for attempt := 1; attempt <= task.RetryCount+1; attempt++ {
		run.Attempts = attempt
		runCtx, cancel := context.WithTimeout(s.ctx, s.options.RunTimeout)
		output, executeErr = s.executor.Execute(runCtx, task, func(value string) error {
			run.Output = value
			return s.store.UpdateRun(runCtx, *run)
		})
		cancel()
		if executeErr == nil || !IsRetryable(executeErr) || attempt > task.RetryCount {
			break
		}
		if !s.waitRetry(time.Duration(attempt) * retryDelayUnit) {
			return output, s.ctx.Err()
		}
	}
	return output, executeErr
}

func (s *Service) waitRetry(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *Service) finish(task Task, run Run, executeErr error) {
	finishedAt := s.options.Now().UTC()
	run.FinishedAt = &finishedAt
	run.Status = RunStatusSuccess
	run.Error = ""
	if executeErr != nil {
		run.Status = RunStatusFailed
		run.Error = executeErr.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.store.UpdateRun(ctx, run); err != nil {
		s.report(fmt.Errorf("finish automatic task run %d: %w", run.ID, err))
		return
	}
	if task.Notify && s.options.Notify != nil {
		if err := s.options.Notify(ctx, task, run); err != nil {
			s.report(fmt.Errorf("notify automatic task run %d: %w", run.ID, err))
			run.Output = appendRunOutput(run.Output, "notification_error="+err.Error())
			if updateErr := s.store.UpdateRun(ctx, run); updateErr != nil {
				s.report(fmt.Errorf("persist automatic task notification failure for run %d: %w", run.ID, updateErr))
			}
		}
	}
}

func appendRunOutput(output, line string) string {
	if output == "" {
		return line
	}
	return output + "\n" + line
}

func (s *Service) finishWithError(run Run, executeErr error) {
	s.finish(Task{ID: run.TaskID}, run, executeErr)
}

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

func IsRetryable(err error) bool {
	var permanent permanentError
	return err != nil && !errors.As(err, &permanent)
}
