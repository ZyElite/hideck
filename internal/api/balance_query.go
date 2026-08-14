package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/balance"
	"github.com/yibaiba/hideck/internal/carrierquery"
	"github.com/yibaiba/hideck/internal/db"
	"github.com/yibaiba/hideck/internal/notify"
)

type balanceQueryRequest struct {
	DeviceID string `json:"device_id" binding:"required"`
}

type manualBalanceRequest struct {
	Amount   string `json:"amount" binding:"required"`
	Currency string `json:"currency"`
}

type carrierRuleStore interface {
	ListCustomCarrierQueryRules() ([]carrierquery.Rule, error)
	SaveCustomCarrierQueryRule(carrierquery.Rule) error
	DeleteCustomCarrierQueryRule(string) error
}

type databaseCarrierRuleStore struct{}

func (databaseCarrierRuleStore) ListCustomCarrierQueryRules() ([]carrierquery.Rule, error) {
	return db.ListCustomCarrierQueryRules()
}

func (databaseCarrierRuleStore) SaveCustomCarrierQueryRule(rule carrierquery.Rule) error {
	return db.SaveCustomCarrierQueryRule(rule)
}

func (databaseCarrierRuleStore) DeleteCustomCarrierQueryRule(id string) error {
	return db.DeleteCustomCarrierQueryRule(id)
}

func (s *Server) handleBalanceQueryStart(c *gin.Context) {
	if !s.requireBalance(c) {
		return
	}
	var request balanceQueryRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "device_id 不能为空"})
		return
	}
	s.startBalanceQuery(c, request.DeviceID)
}

func (s *Server) handleDeviceBalanceQueryStart(c *gin.Context) {
	if !s.requireBalance(c) {
		return
	}
	s.startBalanceQuery(c, c.Param("device_id"))
}

func (s *Server) handleDeviceManualBalancePut(c *gin.Context) {
	if !s.requireBalance(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var request manualBalanceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "手动余额 JSON 无效"})
		return
	}
	query, err := s.balance.SetManualBalance(
		c.Request.Context(), c.Param("device_id"), request.Amount, request.Currency,
	)
	if err != nil {
		writeManualBalanceError(c, err, "手动余额保存失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"query": query})
}

func (s *Server) handleDeviceManualBalanceGet(c *gin.Context) {
	if !s.requireBalance(c) {
		return
	}
	query, found, err := s.balance.GetManualBalance(c.Request.Context(), c.Param("device_id"))
	if err != nil {
		writeManualBalanceError(c, err, "手动余额读取失败")
		return
	}
	if !found {
		c.JSON(http.StatusOK, gin.H{"query": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"query": query})
}

func (s *Server) handleDeviceManualBalanceDelete(c *gin.Context) {
	if !s.requireBalance(c) {
		return
	}
	if _, err := s.balance.ClearManualBalance(c.Request.Context(), c.Param("device_id")); err != nil {
		writeManualBalanceError(c, err, "手动余额清除失败")
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) startBalanceQuery(c *gin.Context, deviceID string) {
	query, err := s.balance.StartQuery(c.Request.Context(), strings.TrimSpace(deviceID))
	if err != nil {
		writeBalanceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"query": query})
}

func (s *Server) handleBalanceQueryList(c *gin.Context) {
	if !s.requireBalance(c) {
		return
	}
	s.listBalanceQueries(c, c.Query("device_id"))
}

func (s *Server) handleDeviceBalanceQueryList(c *gin.Context) {
	if !s.requireBalance(c) {
		return
	}
	s.listBalanceQueries(c, c.Param("device_id"))
}

func (s *Server) listBalanceQueries(c *gin.Context, deviceID string) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || limit < 1 || limit > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "limit 必须是 1 到 200"})
		return
	}
	before, err := optionalTime(c.Query("before"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "before 必须是 RFC3339 时间"})
		return
	}
	queries, err := s.balance.List(c.Request.Context(), strings.TrimSpace(deviceID), limit, before)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"queries": queries})
}

func (s *Server) handleBalanceQueryGet(c *gin.Context) {
	if !s.requireBalance(c) {
		return
	}
	query, found, err := s.balance.Get(c.Request.Context(), c.Param("query_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "余额查询不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"query": query})
}

func (s *Server) handleBalanceRules(c *gin.Context) {
	if !s.requireCarrierRules(c) {
		return
	}
	custom, err := s.carrierRules.ListCustomCarrierQueryRules()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"built_in": carrierquery.BuiltInRules(), "custom": custom})
}

func (s *Server) handleBalanceRulePost(c *gin.Context) {
	s.saveBalanceRule(c, "")
}

func (s *Server) handleBalanceRulePut(c *gin.Context) {
	s.saveBalanceRule(c, c.Param("rule_id"))
}

func (s *Server) saveBalanceRule(c *gin.Context, pathID string) {
	if !s.requireCarrierRules(c) {
		return
	}
	var rule carrierquery.Rule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "规则 JSON 无效"})
		return
	}
	if strings.TrimSpace(pathID) != "" {
		rule.ID = strings.TrimSpace(pathID)
	}
	rule.BuiltIn = false
	if err := s.carrierRules.SaveCustomCarrierQueryRule(rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rule": rule})
}

func (s *Server) handleBalanceRuleDelete(c *gin.Context) {
	if !s.requireCarrierRules(c) {
		return
	}
	if err := s.carrierRules.DeleteCustomCarrierQueryRule(c.Param("rule_id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) requireCarrierRules(c *gin.Context) bool {
	if s.carrierRules != nil {
		return true
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "运营商规则存储未配置"})
	return false
}

func (s *Server) handleBalanceCommand(_ notify.CommandContext, args []string) string {
	deviceID, err := s.resolveBalanceDevice(args)
	if err != nil {
		return "余额查询 / 参数错误\n原因    " + err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	query, err := s.balance.StartQuery(ctx, deviceID)
	if err != nil {
		return "余额查询 / 失败\n设备    " + deviceID + "\n原因    " + err.Error()
	}
	return formatBalanceCommandResult(query)
}

func (s *Server) resolveBalanceDevice(args []string) (string, error) {
	if len(args) > 1 {
		return "", errors.New("用法 /balance [设备ID]")
	}
	if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
		return strings.TrimSpace(args[0]), nil
	}
	workers := s.pool.GetAllWorkers()
	if len(workers) != 1 {
		return "", errors.New("存在多个或没有设备，请指定设备 ID")
	}
	return workers[0].ID, nil
}

func formatBalanceCommandResult(query balance.Query) string {
	if query.State == balance.StateCompleted {
		return fmt.Sprintf("余额查询 / 完成\n设备    %s\n结果    %s\n原文    %s", query.DeviceID, query.Summary, query.RawResponse)
	}
	return fmt.Sprintf("余额查询 / 已发送\n设备    %s\n查询ID  %s\n状态    等待运营商回复", query.DeviceID, query.ID)
}

func (s *Server) requireBalance(c *gin.Context) bool {
	if s.balance != nil {
		return true
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "余额查询服务未配置"})
	return false
}

func optionalTime(raw string) (*time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	return &parsed, err
}

func writeBalanceError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, balance.ErrDeviceNotFound) {
		status = http.StatusNotFound
	} else if errors.Is(err, balance.ErrPendingQuery) || errors.Is(err, balance.ErrIdentityMissing) {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"status": "error", "message": err.Error()})
}

func writeManualBalanceError(c *gin.Context, err error, fallback string) {
	status := http.StatusInternalServerError
	message := fallback
	switch {
	case errors.Is(err, balance.ErrDeviceNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, balance.ErrIdentityMissing):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, balance.ErrInvalidManual):
		status, message = http.StatusBadRequest, err.Error()
	}
	c.JSON(status, gin.H{"status": "error", "message": message})
}
