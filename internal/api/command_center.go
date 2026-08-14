package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/commandcenter"
)

const maxCommandInputBytes = 4096

type commandExecuteRequest struct {
	Input string `json:"input" binding:"required"`
}

func (s *Server) registerCommandCenterRoutes(api *gin.RouterGroup) {
	s.registerApprovedCommandRoutes(api)
	s.registerCompatibilityCommandRoutes(api)
}

func (s *Server) registerApprovedCommandRoutes(api *gin.RouterGroup) {
	commands := api.Group("/command-center")
	commands.GET("/commands", s.handleCommandCatalog)
	commands.POST("/executions", s.handleCommandExecute)
	commands.GET("/events", s.handleCommandEvents)
	commands.GET("/stream", s.handleCommandEventStream)
	commands.GET("/recordings/:recording", s.handleCommandRecording)
	commands.DELETE("/history", s.handleCommandHistoryClear)

	api.GET("/balances", s.handleBalanceQueryList)
	api.POST("/devices/:device_id/balance-queries", s.handleDeviceBalanceQueryStart)
	api.GET("/devices/:device_id/balance-queries", s.handleDeviceBalanceQueryList)
	api.GET("/devices/:device_id/manual-balance", s.handleDeviceManualBalanceGet)
	api.PUT("/devices/:device_id/manual-balance", s.handleDeviceManualBalancePut)
	api.DELETE("/devices/:device_id/manual-balance", s.handleDeviceManualBalanceDelete)
	api.GET("/carrier-query-rules", s.handleBalanceRules)
	api.POST("/carrier-query-rules", s.handleBalanceRulePost)
	api.PUT("/carrier-query-rules/:rule_id", s.handleBalanceRulePut)
	api.DELETE("/carrier-query-rules/:rule_id", s.handleBalanceRuleDelete)
}

func (s *Server) handleCommandRecording(c *gin.Context) {
	name := strings.TrimSpace(c.Param("recording"))
	if !validVoiceRecordingName(name) {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "录音文件名无效"})
		return
	}
	directory := strings.TrimSpace(s.voiceRecordingDirectory)
	if directory == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "通话录音目录未配置"})
		return
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		status := http.StatusInternalServerError
		if os.IsNotExist(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"status": "error", "message": "通话录音目录不可用"})
		return
	}
	defer root.Close()
	file, err := root.Open(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "通话录音不存在"})
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "通话录音不可读"})
		return
	}
	c.Header("Content-Type", "audio/mpeg")
	c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, name))
	c.Header("Cache-Control", "private, no-store")
	http.ServeContent(c.Writer, c.Request, name, stat.ModTime(), file)
}

func validVoiceRecordingName(name string) bool {
	if !strings.HasPrefix(name, "call_") || !strings.EqualFold(filepath.Ext(name), ".mp3") {
		return false
	}
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		if char != '_' && char != '-' && char != '.' {
			return false
		}
	}
	return true
}

func (s *Server) registerCompatibilityCommandRoutes(api *gin.RouterGroup) {
	api.GET("/commands/catalog", s.handleCommandCatalog)
	api.POST("/commands/executions", s.handleCommandExecute)
	api.GET("/commands/events", s.handleCommandEvents)
	api.GET("/commands/events/stream", s.handleCommandEventStream)
	api.DELETE("/commands/history", s.handleCommandHistoryClear)
	api.POST("/balance/queries", s.handleBalanceQueryStart)
	api.GET("/balance/queries", s.handleBalanceQueryList)
	api.GET("/balance/queries/:query_id", s.handleBalanceQueryGet)
	api.GET("/balance/rules", s.handleBalanceRules)
	api.POST("/balance/rules", s.handleBalanceRulePost)
	api.PUT("/balance/rules/:rule_id", s.handleBalanceRulePut)
	api.DELETE("/balance/rules/:rule_id", s.handleBalanceRuleDelete)
}

func (s *Server) handleCommandCatalog(c *gin.Context) {
	if !s.requireCommandCenter(c) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"commands": s.commandCenter.Definitions()})
}

func (s *Server) handleCommandExecute(c *gin.Context) {
	if !s.requireCommandCenter(c) {
		return
	}
	var request commandExecuteRequest
	if err := c.ShouldBindJSON(&request); err != nil || len(request.Input) > maxCommandInputBytes {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "命令输入无效或超过 4096 字节"})
		return
	}
	execution, err := s.commandCenter.Execute(c.Request.Context(), commandcenter.ExecuteRequest{Input: request.Input})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"execution": execution})
}

func (s *Server) handleCommandEvents(c *gin.Context) {
	if !s.requireCommandCenter(c) {
		return
	}
	after, limit, err := commandEventCursor(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	var events []commandcenter.Event
	if rawBefore := strings.TrimSpace(c.Query("before_id")); rawBefore != "" {
		before, parseErr := strconv.ParseUint(rawBefore, 10, 64)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "before_id 无效"})
			return
		}
		events, err = s.commandCenter.ListEventsBefore(c.Request.Context(), before, limit)
	} else {
		events, err = s.commandCenter.ListEvents(c.Request.Context(), after, limit)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}

func (s *Server) handleCommandHistoryClear(c *gin.Context) {
	if !s.requireCommandCenter(c) {
		return
	}
	deleted, err := s.commandCenter.ClearCompleted(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": deleted})
}

func (s *Server) handleCommandEventStream(c *gin.Context) {
	if !s.requireCommandCenter(c) {
		return
	}
	after, _, err := commandEventCursor(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if !commandEventResumeRequested(c) {
		after, err = s.latestCommandEventID(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
	}
	updates, unsubscribe := s.commandCenter.Subscribe()
	defer unsubscribe()
	if err := prepareSSE(c); err != nil {
		return
	}
	if after, err = s.writeCommandBacklog(c, after); err != nil {
		return
	}
	s.streamCommandEvents(c, updates, after)
}

func commandEventResumeRequested(c *gin.Context) bool {
	return strings.TrimSpace(c.GetHeader("Last-Event-ID")) != "" ||
		strings.TrimSpace(c.Query("after_id")) != ""
}

func (s *Server) latestCommandEventID(ctx context.Context) (uint64, error) {
	events, err := s.commandCenter.ListEventsBefore(ctx, 0, 1)
	if err != nil || len(events) == 0 {
		return 0, err
	}
	return events[0].ID, nil
}

func (s *Server) writeCommandBacklog(c *gin.Context, after uint64) (uint64, error) {
	const pageSize = 200
	for {
		events, err := s.commandCenter.ListEvents(c.Request.Context(), after, pageSize)
		if err != nil {
			return after, err
		}
		for _, event := range events {
			if err := writeSSEEvent(c, event); err != nil {
				return after, err
			}
			after = event.ID
		}
		if len(events) < pageSize {
			return after, nil
		}
	}
}

func (s *Server) streamCommandEvents(c *gin.Context, updates <-chan commandcenter.Event, cursor uint64) {
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-s.shutdownCh:
			return
		case event := <-updates:
			if event.ID <= cursor {
				continue
			}
			if event.ID > cursor+1 {
				var err error
				cursor, err = s.writeCommandBacklog(c, cursor)
				if err != nil {
					return
				}
				continue
			}
			if err := writeSSEEvent(c, event); err != nil {
				return
			}
			cursor = event.ID
		case <-heartbeat.C:
			_, _ = fmt.Fprint(c.Writer, ": keepalive\n\n")
			c.Writer.Flush()
		}
	}
}

func (s *Server) requireCommandCenter(c *gin.Context) bool {
	if s.commandCenter != nil {
		return true
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": commandcenter.ErrUnavailable.Error()})
	return false
}

func commandEventCursor(c *gin.Context) (uint64, int, error) {
	raw := strings.TrimSpace(c.Query("after_id"))
	if header := strings.TrimSpace(c.GetHeader("Last-Event-ID")); header != "" {
		raw = header
	}
	var after uint64
	var err error
	if raw != "" {
		after, err = strconv.ParseUint(raw, 10, 64)
	}
	limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limitErr != nil || limit < 1 || limit > 200 {
		return 0, 0, errors.New("事件游标或 limit 无效")
	}
	return after, limit, nil
}

func prepareSSE(c *gin.Context) error {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	if _, err := fmt.Fprint(c.Writer, ": connected\n\n"); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func writeSSEEvent(c *gin.Context, event commandcenter.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "id: %d\nevent: command\ndata: %s\n\n", event.ID, payload); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}
