package handlers

import (
	"net/http"

	"github.com/KubeOrch/core/middleware"
	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type NotificationHandler struct {
	service *services.AlertService
	logger  *logrus.Logger
}

func NewNotificationHandler() *NotificationHandler {
	return &NotificationHandler{
		service: services.GetAlertService(),
		logger:  logrus.New(),
	}
}

// ListChannels returns all notification channels for the current user
func (h *NotificationHandler) ListChannels(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	channels, err := h.service.ListChannels(ctx, userID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list notification channels")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if channels == nil {
		channels = []models.NotificationChannel{}
	}

	c.JSON(http.StatusOK, gin.H{"channels": channels})
}

type channelRequest struct {
	Name    string                 `json:"name" binding:"required"`
	Type    string                 `json:"type" binding:"required"`
	Config  map[string]interface{} `json:"config" binding:"required"`
	Enabled *bool                  `json:"enabled"`
}

// CreateChannel creates a new notification channel
func (h *NotificationHandler) CreateChannel(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var req channelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	channel := &models.NotificationChannel{
		UserID:  userID,
		Name:    req.Name,
		Type:    models.NotificationChannelType(req.Type),
		Config:  req.Config,
		Enabled: enabled,
	}

	ctx := c.Request.Context()
	if err := h.service.CreateChannel(ctx, channel); err != nil {
		h.logger.WithError(err).Error("Failed to create notification channel")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Channel created", "channel": channel})
}

// GetChannel returns a single notification channel
func (h *NotificationHandler) GetChannel(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel ID"})
		return
	}

	ctx := c.Request.Context()
	channel, err := h.service.GetChannel(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
		return
	}

	if channel.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	c.JSON(http.StatusOK, channel)
}

// UpdateChannel updates a notification channel
func (h *NotificationHandler) UpdateChannel(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel ID"})
		return
	}

	ctx := c.Request.Context()
	existing, err := h.service.GetChannel(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
		return
	}
	if existing.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	var req channelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	existing.Name = req.Name
	existing.Type = models.NotificationChannelType(req.Type)
	existing.Config = req.Config
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	if err := h.service.UpdateChannel(ctx, existing); err != nil {
		h.logger.WithError(err).Error("Failed to update notification channel")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Channel updated", "channel": existing})
}

// DeleteChannel deletes a notification channel
func (h *NotificationHandler) DeleteChannel(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel ID"})
		return
	}

	ctx := c.Request.Context()
	channel, err := h.service.GetChannel(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
		return
	}
	if channel.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	if err := h.service.DeleteChannel(ctx, id); err != nil {
		h.logger.WithError(err).Error("Failed to delete notification channel")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Channel deleted"})
}

// TestChannel sends a test notification to a channel
func (h *NotificationHandler) TestChannel(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid channel ID"})
		return
	}

	ctx := c.Request.Context()
	channel, err := h.service.GetChannel(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
		return
	}
	if channel.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	if err := h.service.TestChannel(ctx, channel); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Test failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Test notification sent successfully"})
}
