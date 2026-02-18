package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ListUserNotificationsHandler returns paginated in-app notifications for the current user.
func ListUserNotificationsHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	notifications, total, err := services.GetNotifications(userID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch notifications"})
		return
	}

	unreadCount, err := services.GetUnreadCount(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch unread count"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"notifications": notifications,
		"total":         total,
		"unreadCount":   unreadCount,
	})
}

// UserNotificationUnreadCountHandler returns the unread notification count for the current user.
func UserNotificationUnreadCountHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	count, err := services.GetUnreadCount(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch unread count"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"unreadCount": count})
}

// MarkUserNotificationReadHandler marks a single notification as read.
func MarkUserNotificationReadHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	notifID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid notification ID"})
		return
	}

	if err := services.MarkNotificationRead(notifID, userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Notification not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Notification marked as read"})
}

// MarkAllUserNotificationsReadHandler marks all notifications as read for the current user.
func MarkAllUserNotificationsReadHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	count, err := services.MarkAllNotificationsRead(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to mark notifications as read"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "All notifications marked as read", "count": count})
}

// StreamUserNotificationsHandler streams real-time in-app notifications via SSE.
func StreamUserNotificationsHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
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

	// Monitor client disconnect
	go func() {
		<-reqCtx.Done()
		logrus.WithField("user_id", userID.Hex()).Info("Client disconnected from notifications stream")
		cancel()
	}()

	// Subscribe to user's notification stream
	broadcaster := services.GetSSEBroadcaster()
	streamKey := fmt.Sprintf("notifications:%s", userID.Hex())
	eventChan := broadcaster.Subscribe(streamKey, 10)
	defer broadcaster.Unsubscribe(streamKey, eventChan)

	logrus.WithField("user_id", userID.Hex()).Info("Client connected to notifications stream")

	// Send initial connected event with unread count
	unreadCount, _ := services.GetUnreadCount(userID)
	initialData, _ := json.Marshal(map[string]interface{}{
		"unreadCount": unreadCount,
	})
	c.SSEvent("connected", string(initialData))
	c.Writer.Flush()

	// Listen for events
	for {
		select {
		case <-streamCtx.Done():
			return

		case event, ok := <-eventChan:
			if !ok {
				c.SSEvent("complete", "Stream closed")
				c.Writer.Flush()
				return
			}

			if event.Type != "notification" {
				continue
			}

			eventJSON, err := json.Marshal(event.Data)
			if err != nil {
				logrus.WithError(err).Warn("Failed to marshal notification event")
				continue
			}

			c.SSEvent(event.EventType, string(eventJSON))
			c.Writer.Flush()

			if c.Errors.Last() != nil {
				logrus.WithError(c.Errors.Last()).Warn("Error writing notification SSE event")
				return
			}
		}
	}
}
