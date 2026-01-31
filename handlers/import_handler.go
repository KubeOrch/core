package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ImportHandler handles import-related requests
type ImportHandler struct {
	importService *services.ImportService
	logger        *logrus.Logger
}

// NewImportHandler creates a new import handler
func NewImportHandler() *ImportHandler {
	return &ImportHandler{
		importService: services.GetImportService(),
		logger:        logrus.New(),
	}
}

// AnalyzeImportRequest is the request body for analyze endpoint
type AnalyzeImportRequest struct {
	Source      models.ImportSource `json:"source" binding:"required"`
	URL         string              `json:"url"`
	Branch      string              `json:"branch"`
	FileContent string              `json:"fileContent"`
	FileName    string              `json:"fileName"`
	Namespace   string              `json:"namespace"`
	WorkflowID  string              `json:"workflowId"`
}

// AnalyzeImportHandler analyzes import source and returns suggested nodes
// POST /v1/api/import/analyze
// Returns either immediate analysis (sync) or sessionId (async) for repos requiring clone
func (h *ImportHandler) AnalyzeImportHandler(c *gin.Context) {
	// Get user ID for async imports
	userIDStr, exists := c.Get("userID")
	var userObjID primitive.ObjectID
	if exists {
		var err error
		userObjID, err = primitive.ObjectIDFromHex(userIDStr.(string))
		if err != nil {
			userObjID = primitive.NilObjectID
		}
	}

	var req AnalyzeImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// Convert to models.ImportRequest
	importReq := &models.ImportRequest{
		Source:      req.Source,
		URL:         req.URL,
		Branch:      req.Branch,
		FileContent: req.FileContent,
		FileName:    req.FileName,
		Namespace:   req.Namespace,
		WorkflowID:  req.WorkflowID,
	}

	// Try fast path first (direct file fetch or file upload)
	analysis, err := h.importService.TryFastImport(c.Request.Context(), importReq)
	if err != nil {
		h.logger.WithError(err).Error("Failed to analyze import (fast path)")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// If fast path succeeded, return immediately
	if analysis != nil {
		c.JSON(http.StatusOK, gin.H{
			"async":    false,
			"analysis": analysis,
		})
		return
	}

	// Fast path failed, need to clone - start async import
	h.logger.Info("Fast import path failed, starting async import with streaming")

	session, err := h.importService.StartAsyncImport(c.Request.Context(), importReq, userObjID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to start async import")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"async":     true,
		"sessionId": session.ID.Hex(),
		"message":   "Cloning repository...",
	})
}

// ApplyImportRequest is the request body for apply endpoint
type ApplyImportRequest struct {
	WorkflowID string                          `json:"workflowId" binding:"required"`
	Nodes      []models.WorkflowNode           `json:"nodes" binding:"required"`
	Edges      []models.WorkflowEdge           `json:"edges"`
	Positions  map[string]models.NodePosition  `json:"positions"`
}

// ApplyImportHandler applies import to a workflow
// POST /v1/api/import/apply
func (h *ImportHandler) ApplyImportHandler(c *gin.Context) {
	userIDStr, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	userObjID, err := primitive.ObjectIDFromHex(userIDStr.(string))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user ID"})
		return
	}

	var req ApplyImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// Convert to models.ApplyImportRequest
	applyReq := &models.ApplyImportRequest{
		WorkflowID: req.WorkflowID,
		Nodes:      req.Nodes,
		Edges:      req.Edges,
		Positions:  req.Positions,
	}

	// Apply the import
	workflow, err := h.importService.ApplyImport(c.Request.Context(), applyReq, userObjID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to apply import")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workflow": workflow,
		"message":  "Import applied successfully",
	})
}

// CreateWorkflowFromImportRequest is the request for creating a new workflow from import
type CreateWorkflowFromImportRequest struct {
	Name        string               `json:"name" binding:"required"`
	ClusterID   string               `json:"clusterId" binding:"required"`
	Analysis    models.ImportAnalysis `json:"analysis" binding:"required"`
}

// CreateWorkflowFromImportHandler creates a new workflow from imported content
// POST /v1/api/import/create-workflow
func (h *ImportHandler) CreateWorkflowFromImportHandler(c *gin.Context) {
	userIDStr, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	userObjID, err := primitive.ObjectIDFromHex(userIDStr.(string))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user ID"})
		return
	}

	var req CreateWorkflowFromImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// Create workflow from import
	workflow, err := h.importService.CreateWorkflowFromImport(
		c.Request.Context(),
		req.Name,
		&req.Analysis,
		userObjID,
		req.ClusterID,
	)
	if err != nil {
		h.logger.WithError(err).Error("Failed to create workflow from import")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workflow": workflow,
		"message":  "Workflow created successfully from import",
	})
}

// UploadComposeHandler handles file upload for docker-compose
// POST /v1/api/import/upload
func (h *ImportHandler) UploadComposeHandler(c *gin.Context) {
	// Get the uploaded file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded: " + err.Error()})
		return
	}
	defer file.Close()

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".yml" && ext != ".yaml" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid file type. Only .yml and .yaml files are allowed"})
		return
	}

	// Validate file size (max 1MB)
	if header.Size > 1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File too large. Maximum size is 1MB"})
		return
	}

	// Read file content
	content, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read file: " + err.Error()})
		return
	}

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(content)

	// Get namespace from form
	namespace := c.PostForm("namespace")
	if namespace == "" {
		namespace = "default"
	}

	// Analyze the content
	importReq := &models.ImportRequest{
		Source:      models.ImportSourceFile,
		FileContent: encoded,
		FileName:    header.Filename,
		Namespace:   namespace,
	}

	analysis, err := h.importService.AnalyzeImport(c.Request.Context(), importReq)
	if err != nil {
		h.logger.WithError(err).Error("Failed to analyze uploaded file")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"analysis":    analysis,
		"fileName":    header.Filename,
		"fileSize":    header.Size,
		"fileContent": encoded, // Return so frontend can use it later
	})
}

// GetImportSessionHandler retrieves an import session status and result
// GET /v1/api/import/:id
func (h *ImportHandler) GetImportSessionHandler(c *gin.Context) {
	sessionID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid session ID"})
		return
	}

	session, err := h.importService.GetImportSession(c.Request.Context(), sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Import session not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session": session,
	})
}

// StreamImportLogsHandler streams import logs via SSE
// GET /v1/api/import/:id/stream
func (h *ImportHandler) StreamImportLogsHandler(c *gin.Context) {
	sessionID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid session ID"})
		return
	}

	// Verify session exists
	session, err := h.importService.GetImportSession(c.Request.Context(), sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Import session not found"})
		return
	}

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Subscribe to import events
	broadcaster := services.GetSSEBroadcaster()
	streamKey := models.GetImportStreamKey(sessionID)
	eventChan := broadcaster.Subscribe(streamKey, 100)

	// Ensure cleanup on disconnect
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	defer broadcaster.Unsubscribe(streamKey, eventChan)

	// Send initial session state as metadata
	sessionJSON, _ := json.Marshal(session)
	c.SSEvent("metadata", string(sessionJSON))
	c.Writer.Flush()

	// If session is already complete, send complete event and close
	if session.IsTerminal() {
		if session.Status == models.ImportStatusCompleted && session.Analysis != nil {
			analysisJSON, _ := json.Marshal(session.Analysis)
			c.SSEvent("complete", string(analysisJSON))
		} else {
			c.SSEvent("failed", `{"error": "`+session.ErrorMessage+`"}`)
		}
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
				h.logger.WithError(err).Warn("Failed to marshal import event")
				continue
			}

			c.SSEvent(event.EventType, string(eventJSON))
			c.Writer.Flush()

			// If this is a terminal event, close the stream
			if event.EventType == "complete" || event.EventType == "failed" {
				return
			}
		}
	}
}
