package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/phone"
)

const phoneEventHeartbeatInterval = 15 * time.Second

func (s *Server) handlePhoneEvents(c *gin.Context) {
	if !s.requirePhone(c) {
		return
	}
	backlog, stream, cancel := s.phone.Subscribe(phoneLastEventID(c))
	defer cancel()
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)
	for _, event := range backlog {
		writePhoneEvent(c, event)
	}
	c.Writer.Flush()
	heartbeat := time.NewTicker(phoneEventHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case event, ok := <-stream:
			if !ok {
				return
			}
			writePhoneEvent(c, event)
			c.Writer.Flush()
		case <-heartbeat.C:
			_, _ = fmt.Fprint(c.Writer, ": heartbeat\n\n")
			c.Writer.Flush()
		case <-c.Request.Context().Done():
			return
		case <-s.shutdownCh:
			return
		}
	}
}

func writePhoneEvent(c *gin.Context, event phone.Event) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(c.Writer, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Type, payload)
}

func phoneLastEventID(c *gin.Context) uint64 {
	value := strings.TrimSpace(c.GetHeader("Last-Event-ID"))
	if value == "" {
		value = strings.TrimSpace(c.Query("after_id"))
	}
	result, _ := strconv.ParseUint(value, 10, 64)
	return result
}
