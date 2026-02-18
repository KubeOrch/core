package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/KubeOrch/core/middleware"
	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type AlertHandler struct {
	service *services.AlertService
	logger  *logrus.Logger
}

func NewAlertHandler() *AlertHandler {
	return &AlertHandler{
		service: services.GetAlertService(),
		logger:  logrus.New(),
	}
}

// GetOverview returns alert overview stats
func (h *AlertHandler) GetOverview(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	stats, err := h.service.GetOverviewStats(ctx, userID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get alert overview")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// ListRules returns alert rules for the current user
func (h *AlertHandler) ListRules(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ruleType := c.Query("type")
	severity := c.Query("severity")
	var enabled *bool
	if e := c.Query("enabled"); e != "" {
		b := e == "true"
		enabled = &b
	}

	ctx := c.Request.Context()
	rules, err := h.service.ListRules(ctx, userID, ruleType, severity, enabled)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list alert rules")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if rules == nil {
		rules = []models.AlertRule{}
	}

	c.JSON(http.StatusOK, gin.H{"rules": rules})
}

type createRuleRequest struct {
	Name                   string                   `json:"name" binding:"required"`
	Description            string                   `json:"description"`
	Type                   models.AlertRuleType     `json:"type" binding:"required"`
	Severity               models.AlertSeverity     `json:"severity" binding:"required"`
	Conditions             []models.AlertCondition  `json:"conditions" binding:"required"`
	ClusterIDs             []string                 `json:"clusterIds"`
	WorkflowIDs            []string                 `json:"workflowIds"`
	ResourceTypes          []string                 `json:"resourceTypes"`
	Namespaces             []string                 `json:"namespaces"`
	NotificationChannelIDs []string                 `json:"notificationChannelIds"`
	EvaluationInterval     int                      `json:"evaluationInterval"`
	CooldownPeriod         int                      `json:"cooldownPeriod"`
}

// CreateRule creates a new custom alert rule
func (h *AlertHandler) CreateRule(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var req createRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	channelIDs := make([]primitive.ObjectID, 0, len(req.NotificationChannelIDs))
	for _, id := range req.NotificationChannelIDs {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid notification channel ID: %s", id)})
			return
		}
		channelIDs = append(channelIDs, oid)
	}

	rule := &models.AlertRule{
		UserID:                 userID,
		Name:                   req.Name,
		Description:            req.Description,
		Type:                   req.Type,
		Severity:               req.Severity,
		Enabled:                true,
		Conditions:             req.Conditions,
		ClusterIDs:             req.ClusterIDs,
		WorkflowIDs:            req.WorkflowIDs,
		ResourceTypes:          req.ResourceTypes,
		Namespaces:             req.Namespaces,
		NotificationChannelIDs: channelIDs,
		EvaluationInterval:     req.EvaluationInterval,
		CooldownPeriod:         req.CooldownPeriod,
	}

	ctx := c.Request.Context()
	if err := h.service.CreateRule(ctx, rule); err != nil {
		h.logger.WithError(err).Error("Failed to create alert rule")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Alert rule created", "rule": rule})
}

// GetRule returns a single alert rule
func (h *AlertHandler) GetRule(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid rule ID"})
		return
	}

	ctx := c.Request.Context()
	rule, err := h.service.GetRule(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Rule not found"})
		return
	}

	if rule.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	c.JSON(http.StatusOK, rule)
}

// UpdateRule updates an existing alert rule
func (h *AlertHandler) UpdateRule(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid rule ID"})
		return
	}

	ctx := c.Request.Context()
	existing, err := h.service.GetRule(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Rule not found"})
		return
	}
	if existing.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	var req createRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	channelIDs := make([]primitive.ObjectID, 0, len(req.NotificationChannelIDs))
	for _, cid := range req.NotificationChannelIDs {
		oid, err := primitive.ObjectIDFromHex(cid)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid notification channel ID: %s", cid)})
			return
		}
		channelIDs = append(channelIDs, oid)
	}

	existing.Name = req.Name
	existing.Description = req.Description
	existing.Type = req.Type
	existing.Severity = req.Severity
	existing.Conditions = req.Conditions
	existing.ClusterIDs = req.ClusterIDs
	existing.WorkflowIDs = req.WorkflowIDs
	existing.ResourceTypes = req.ResourceTypes
	existing.Namespaces = req.Namespaces
	existing.NotificationChannelIDs = channelIDs
	existing.EvaluationInterval = req.EvaluationInterval
	existing.CooldownPeriod = req.CooldownPeriod

	if err := h.service.UpdateRule(ctx, existing); err != nil {
		h.logger.WithError(err).Error("Failed to update alert rule")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Rule updated", "rule": existing})
}

// DeleteRule deletes an alert rule
func (h *AlertHandler) DeleteRule(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid rule ID"})
		return
	}

	ctx := c.Request.Context()
	rule, err := h.service.GetRule(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Rule not found"})
		return
	}
	if rule.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	if err := h.service.DeleteRule(ctx, id); err != nil {
		h.logger.WithError(err).Error("Failed to delete alert rule")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Rule deleted"})
}

type toggleRequest struct {
	Enabled bool `json:"enabled"`
}

// ToggleRule enables or disables an alert rule
func (h *AlertHandler) ToggleRule(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid rule ID"})
		return
	}

	ctx := c.Request.Context()
	rule, err := h.service.GetRule(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Rule not found"})
		return
	}
	if rule.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	var req toggleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if err := h.service.ToggleRule(ctx, id, req.Enabled); err != nil {
		h.logger.WithError(err).Error("Failed to toggle alert rule")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Rule toggled", "enabled": req.Enabled})
}

// ListTemplates returns all predefined alert templates
func (h *AlertHandler) ListTemplates(c *gin.Context) {
	templates := services.GetAllTemplates()
	c.JSON(http.StatusOK, gin.H{"templates": templates})
}

type enableTemplateRequest struct {
	ClusterIDs  []string `json:"clusterIds"`
	WorkflowIDs []string `json:"workflowIds"`
	Namespaces  []string `json:"namespaces"`
}

// EnableTemplate enables a predefined template for the user
func (h *AlertHandler) EnableTemplate(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	templateID := c.Param("templateId")
	if templateID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Template ID is required"})
		return
	}

	var req enableTemplateRequest
	// Body is optional
	_ = c.ShouldBindJSON(&req)

	ctx := c.Request.Context()
	rule, err := h.service.EnableTemplate(ctx, userID, templateID, req.ClusterIDs, req.WorkflowIDs, req.Namespaces)
	if err != nil {
		h.logger.WithError(err).Error("Failed to enable template")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Template enabled", "rule": rule})
}

// ListEvents returns alert events for the current user
func (h *AlertHandler) ListEvents(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	severity := c.Query("severity")
	status := c.Query("status")
	ruleType := c.Query("type")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	ctx := c.Request.Context()
	events, total, err := h.service.ListEvents(ctx, userID, severity, status, ruleType, page, limit)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list alert events")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if events == nil {
		events = []models.AlertEvent{}
	}

	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"total":  total,
		"page":   page,
		"limit":  limit,
	})
}

// AcknowledgeEvent acknowledges a firing alert event
func (h *AlertHandler) AcknowledgeEvent(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid event ID"})
		return
	}

	ctx := c.Request.Context()
	if err := h.service.AcknowledgeEvent(ctx, id, userID); err != nil {
		h.logger.WithError(err).Error("Failed to acknowledge event")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Event acknowledged"})
}

// ResolveEvent resolves an alert event
func (h *AlertHandler) ResolveEvent(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid event ID"})
		return
	}

	ctx := c.Request.Context()
	if err := h.service.ResolveEvent(ctx, id, userID); err != nil {
		h.logger.WithError(err).Error("Failed to resolve event")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Event resolved"})
}

// FireTestAlert creates a test alert event for testing the history tab
func (h *AlertHandler) FireTestAlert(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()

	// Create a temporary rule-like structure for the test
	testRule := &models.AlertRule{
		UserID:   userID,
		Name:     "Test Alert",
		Type:     models.AlertRuleTypeCluster,
		Severity: models.AlertSeverityWarning,
	}

	details := map[string]interface{}{
		"source": "manual_test",
	}

	if err := h.service.FireAlert(ctx, testRule, "This is a test alert to verify the History tab", details); err != nil {
		h.logger.WithError(err).Error("Failed to fire test alert")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Test alert fired"})
}

// StreamAlerts streams real-time alert events via SSE
func (h *AlertHandler) StreamAlerts(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	reqCtx := c.Request.Context()
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-reqCtx.Done()
		cancel()
	}()

	broadcaster := services.GetSSEBroadcaster()
	streamKey := fmt.Sprintf("alerts:%s", userID.Hex())
	eventChan := broadcaster.Subscribe(streamKey, 10)
	defer broadcaster.Unsubscribe(streamKey, eventChan)

	// Send initial connected event
	c.SSEvent("connected", `{"status":"connected"}`)
	c.Writer.Flush()

	for {
		select {
		case <-streamCtx.Done():
			return
		case event, ok := <-eventChan:
			if !ok {
				return
			}
			if event.Type != "alerts" {
				continue
			}
			eventJSON, err := json.Marshal(event)
			if err != nil {
				continue
			}
			c.SSEvent(event.EventType, string(eventJSON))
			c.Writer.Flush()
		}
	}
}
