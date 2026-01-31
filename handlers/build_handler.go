package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// StartBuildHandler initiates a new build
// POST /builds/start
func StartBuildHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	var req models.StartBuildRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	// Validate required fields
	if req.RepoURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repoUrl is required"})
		return
	}
	if req.RegistryID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "registryId is required"})
		return
	}
	if req.ImageName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "imageName is required"})
		return
	}

	// Set defaults
	if req.Branch == "" {
		req.Branch = "main"
	}
	if req.ImageTag == "" {
		req.ImageTag = "latest"
	}
	if req.BuildContext == "" {
		req.BuildContext = "."
	}

	buildService := services.GetBuildService()
	build, err := buildService.StartBuild(c.Request.Context(), req, userID)
	if err != nil {
		logrus.WithError(err).Error("Failed to start build")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start build: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"build":   build,
		"message": "Build started successfully",
	})
}

// GetBuildHandler retrieves a build by ID
// GET /builds/:id
func GetBuildHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	buildID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid build ID"})
		return
	}

	buildService := services.GetBuildService()
	build, err := buildService.GetBuild(c.Request.Context(), buildID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Build not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"build": build})
}

// ListBuildsHandler lists builds for the current user
// GET /builds
func ListBuildsHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	// Parse pagination params
	limit := 20
	offset := 0

	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	buildService := services.GetBuildService()
	builds, total, err := buildService.ListBuilds(c.Request.Context(), userID, limit, offset)
	if err != nil {
		logrus.WithError(err).Error("Failed to list builds")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list builds"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"builds": builds,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// StreamBuildLogsHandler streams build logs via SSE
// GET /builds/:id/stream
func StreamBuildLogsHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	buildID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid build ID"})
		return
	}

	buildService := services.GetBuildService()

	// Verify ownership
	build, err := buildService.GetBuild(c.Request.Context(), buildID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Build not found"})
		return
	}

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Subscribe to build events
	broadcaster := services.GetSSEBroadcaster()
	streamKey := services.GetBuildStreamKey(buildID)
	eventChan := broadcaster.Subscribe(streamKey, 100) // Larger buffer for logs

	// Ensure cleanup on disconnect
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	defer broadcaster.Unsubscribe(streamKey, eventChan)

	// Send initial build state as metadata
	buildJSON, _ := json.Marshal(build)
	c.SSEvent("metadata", string(buildJSON))
	c.Writer.Flush()

	// If build is already complete, send complete event and close
	if build.IsTerminal() {
		c.SSEvent("complete", `{"status": "`+string(build.Status)+`"}`)
		c.Writer.Flush()
		return
	}

	// Stream events
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventChan:
			if !ok {
				c.SSEvent("complete", `{"status": "stream_closed"}`)
				c.Writer.Flush()
				return
			}

			eventJSON, err := json.Marshal(event)
			if err != nil {
				logrus.WithError(err).Warn("Failed to marshal build event")
				continue
			}

			c.SSEvent(event.EventType, string(eventJSON))
			c.Writer.Flush()

			// If this is a terminal event, close the stream
			if event.EventType == "complete" || event.EventType == "failed" || event.EventType == "cancelled" {
				return
			}
		}
	}
}

// CancelBuildHandler cancels an in-progress build
// POST /builds/:id/cancel
func CancelBuildHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	buildID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid build ID"})
		return
	}

	buildService := services.GetBuildService()
	err = buildService.CancelBuild(c.Request.Context(), buildID, userID)
	if err != nil {
		logrus.WithError(err).Error("Failed to cancel build")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Build cancelled successfully"})
}
