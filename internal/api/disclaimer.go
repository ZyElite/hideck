package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/db"
)

const (
	disclaimerVersion          = "1"
	disclaimerConfirmationText = "我同意并确认"
)

type disclaimerAcceptanceStore interface {
	Status(context.Context, string) (time.Time, bool, error)
	Accept(context.Context, string, time.Time) (time.Time, error)
}

type disclaimerStatusPayload struct {
	Accepted   bool       `json:"accepted"`
	AcceptedAt *time.Time `json:"accepted_at"`
	Version    string     `json:"version"`
}

type acceptDisclaimerRequest struct {
	Confirmation string `json:"confirmation"`
}

func (s *Server) handleGetDisclaimerStatus(c *gin.Context) {
	store := s.disclaimerAcceptances
	if store == nil {
		writeDisclaimerStoreError(c, db.ErrDisclaimerDatabaseUnavailable)
		return
	}
	acceptedAt, accepted, err := store.Status(c.Request.Context(), disclaimerVersion)
	if err != nil {
		writeDisclaimerStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, disclaimerStatus(accepted, acceptedAt))
}

func (s *Server) handleAcceptDisclaimer(c *gin.Context) {
	var request acceptDisclaimerRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "参数错误"})
		return
	}
	if request.Confirmation != disclaimerConfirmationText {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "确认文字不匹配"})
		return
	}
	store := s.disclaimerAcceptances
	if store == nil {
		writeDisclaimerStoreError(c, db.ErrDisclaimerDatabaseUnavailable)
		return
	}
	acceptedAt, err := store.Accept(c.Request.Context(), disclaimerVersion, time.Now().UTC())
	if err != nil {
		writeDisclaimerStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, disclaimerStatus(true, acceptedAt))
}

func disclaimerStatus(accepted bool, acceptedAt time.Time) disclaimerStatusPayload {
	payload := disclaimerStatusPayload{Accepted: accepted, Version: disclaimerVersion}
	if accepted {
		acceptedAt = acceptedAt.UTC()
		payload.AcceptedAt = &acceptedAt
	}
	return payload
}

func writeDisclaimerStoreError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := "读取免责声明状态失败: " + err.Error()
	if errors.Is(err, db.ErrDisclaimerDatabaseUnavailable) {
		status = http.StatusServiceUnavailable
		message = "免责声明数据库未初始化"
	}
	c.JSON(status, gin.H{"status": "error", "message": message})
}
