package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/commandcenter"
)

const maxCommandInputBytes = 4096

type commandExecuteRequest struct {
	Input string `json:"input" binding:"required"`
}

func (s *Server) registerCommandCenterRoutes(api *gin.RouterGroup) {
	api.GET("/commands/catalog", s.handleCommandCatalog)
	api.POST("/commands/executions", s.handleCommandExecute)
	api.GET("/commands/events", s.handleCommandEvents)
	api.GET("/commands/events/stream", s.handleCommandEventStream)
	api.DELETE("/commands/history", s.handleCommandHistoryClear)
	api.POST("/balance/queries", s.handleBalanceQueryStart)
	api.GET("/balance/queries", s.handleBalanceQueryList)
	api.GET("/balance/queries/:query_id", s.handleBalanceQueryGet)
	api.GET("/balance/rules", s.handleBalanceRules)
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
	updates, unsubscribe := s.commandCenter.Subscribe()
	defer unsubscribe()
	prepareSSE(c)
	if after, err = s.writeCommandBacklog(c, after); err != nil {
		return
	}
	s.streamCommandEvents(c, updates, after)
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

func prepareSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	c.Writer.Flush()
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
