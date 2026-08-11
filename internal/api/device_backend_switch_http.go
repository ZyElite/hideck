package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) deviceBackendSwitcher() deviceBackendSwitcher {
	if s.backendSwitch != nil {
		return s.backendSwitch
	}
	return newDeviceBackendSwitchService(s.pool, s.configPath)
}

func respondBackendSwitchFailure(c *gin.Context, err error) {
	status := http.StatusConflict
	payload := gin.H{"status": "error", "message": err.Error()}
	var failure *backendSwitchFailure
	if !errors.As(err, &failure) {
		c.JSON(http.StatusInternalServerError, payload)
		return
	}
	payload["stage"] = failure.Stage
	payload["backend_switch"] = failure.Result
	switch failure.Stage {
	case "validate":
		status = http.StatusBadRequest
	case "initialize", "persist_config", "start_worker":
		status = http.StatusInternalServerError
	}
	c.JSON(status, payload)
}
