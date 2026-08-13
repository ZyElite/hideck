package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/automation"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/db"
	"github.com/iniwex5/vohive/pkg/logger"
)

type automaticTaskService interface {
	Start(context.Context) error
	Stop(context.Context) error
	SaveTask(context.Context, automation.Task) (automation.Task, error)
	GetTask(context.Context, uint64) (automation.Task, error)
	ListTasks(context.Context) ([]automation.Task, error)
	DeleteTask(context.Context, uint64) error
	RunNow(context.Context, uint64) (automation.Run, error)
	ListRuns(context.Context, uint64, int, int) ([]automation.Run, int64, error)
}

func (s *Server) initializeAutomaticTasks() {
	if s == nil || db.DB == nil || s.pool == nil {
		return
	}
	service := automation.NewService(
		db.NewAutomaticTaskStore(db.DB),
		&automaticTaskExecutor{server: s},
		automation.Options{
			OnError: func(err error) { logger.Error("自动任务后台执行失败", "err", err) },
			Notify:  s.notifyAutomaticTaskRun,
		},
	)
	if err := service.Start(context.Background()); err != nil {
		logger.Error("自动任务调度器启动失败", "err", err)
		return
	}
	s.automaticTasks = service
}

func (s *Server) notifyAutomaticTaskRun(_ context.Context, task automation.Task, run automation.Run) error {
	if s == nil || s.notifyMgr == nil {
		return errors.New("notification manager is unavailable")
	}
	message := fmt.Sprintf("自动任务 / %s\n设备  %s\n状态  %s", task.Name, task.DeviceID, run.Status)
	if run.Error != "" {
		message += "\n错误  " + run.Error
	}
	s.notifyMgr.NotifyRaw(message)
	return nil
}

func (s *Server) registerAutomaticTaskRoutes(api *gin.RouterGroup) {
	api.GET("/automatic-tasks", s.handleListAutomaticTasks)
	api.POST("/automatic-tasks", s.handleCreateAutomaticTask)
	api.GET("/automatic-tasks/:task_id", s.handleGetAutomaticTask)
	api.PUT("/automatic-tasks/:task_id", s.handleUpdateAutomaticTask)
	api.DELETE("/automatic-tasks/:task_id", s.handleDeleteAutomaticTask)
	api.POST("/automatic-tasks/:task_id/actions/run", s.handleRunAutomaticTask)
	api.GET("/automatic-task-runs", s.handleListAutomaticTaskRuns)
}

func (s *Server) handleListAutomaticTasks(c *gin.Context) {
	if !s.requireAutomaticTasks(c) {
		return
	}
	tasks, err := s.automaticTasks.ListTasks(c.Request.Context())
	if err != nil {
		writeAutomaticTaskError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

func (s *Server) handleGetAutomaticTask(c *gin.Context) {
	id, ok := automaticTaskID(c)
	if !ok || !s.requireAutomaticTasks(c) {
		return
	}
	task, err := s.automaticTasks.GetTask(c.Request.Context(), id)
	if err != nil {
		writeAutomaticTaskError(c, err)
		return
	}
	c.JSON(http.StatusOK, task)
}

func (s *Server) handleCreateAutomaticTask(c *gin.Context) {
	if !s.requireAutomaticTasks(c) {
		return
	}
	var task automation.Task
	if err := c.ShouldBindJSON(&task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	task.ID = 0
	if err := validateAutomaticTaskDevice(task); err != nil {
		writeAutomaticTaskError(c, err)
		return
	}
	created, err := s.automaticTasks.SaveTask(c.Request.Context(), task)
	if err != nil {
		writeAutomaticTaskError(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (s *Server) handleUpdateAutomaticTask(c *gin.Context) {
	id, ok := automaticTaskID(c)
	if !ok || !s.requireAutomaticTasks(c) {
		return
	}
	var task automation.Task
	if err := c.ShouldBindJSON(&task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	task.ID = id
	if err := validateAutomaticTaskDevice(task); err != nil {
		writeAutomaticTaskError(c, err)
		return
	}
	updated, err := s.automaticTasks.SaveTask(c.Request.Context(), task)
	if err != nil {
		writeAutomaticTaskError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (s *Server) handleDeleteAutomaticTask(c *gin.Context) {
	id, ok := automaticTaskID(c)
	if !ok || !s.requireAutomaticTasks(c) {
		return
	}
	if err := s.automaticTasks.DeleteTask(c.Request.Context(), id); err != nil {
		writeAutomaticTaskError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) handleRunAutomaticTask(c *gin.Context) {
	id, ok := automaticTaskID(c)
	if !ok || !s.requireAutomaticTasks(c) {
		return
	}
	run, err := s.automaticTasks.RunNow(c.Request.Context(), id)
	if err != nil {
		writeAutomaticTaskError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, run)
}

func (s *Server) handleListAutomaticTaskRuns(c *gin.Context) {
	if !s.requireAutomaticTasks(c) {
		return
	}
	taskID, err := parseOptionalUint64(c.Query("task_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "task_id must be an unsigned integer"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, offset = normalizeAutomaticTaskPagination(limit, offset)
	runs, total, err := s.automaticTasks.ListRuns(c.Request.Context(), taskID, limit, offset)
	if err != nil {
		writeAutomaticTaskError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs, "total": total, "limit": limit, "offset": offset})
}

func normalizeAutomaticTaskPagination(limit, offset int) (int, int) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func validateAutomaticTaskDevice(task automation.Task) error {
	deviceID := strings.TrimSpace(task.DeviceID)
	deviceConfig, err := config.GetDeviceByID(deviceID)
	if err != nil {
		return fmt.Errorf("resolve device %s: %w", deviceID, err)
	}
	if deviceConfig == nil {
		return fmt.Errorf("%w: device %s is not configured", automation.ErrInvalidTask, deviceID)
	}
	if strings.EqualFold(deviceConfig.DeviceBackend, "pcsc") && task.Environment != automation.EnvironmentVoWiFi {
		return fmt.Errorf("%w: PCSC reader devices only support the vowifi environment", automation.ErrInvalidTask)
	}
	return nil
}

func (s *Server) requireAutomaticTasks(c *gin.Context) bool {
	if s != nil && s.automaticTasks != nil {
		return true
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "automatic task service is unavailable"})
	return false
}

func automaticTaskID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("task_id")), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "invalid task_id"})
		return 0, false
	}
	return id, true
}

func parseOptionalUint64(value string) (uint64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	id, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || id == 0 {
		return 0, errors.New("value must be a positive integer")
	}
	return id, nil
}

func writeAutomaticTaskError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, automation.ErrInvalidTask):
		status = http.StatusBadRequest
	case errors.Is(err, automation.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, automation.ErrNotStarted):
		status = http.StatusServiceUnavailable
	case errors.Is(err, automation.ErrTaskBusy):
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"status": "error", "message": err.Error()})
}
