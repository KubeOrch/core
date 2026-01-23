package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/pkg/template"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// getUserIDFromContext extracts and validates the user ID from the gin context
func getUserIDFromContext(c *gin.Context) (primitive.ObjectID, bool) {
	userIDStr, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return primitive.NilObjectID, false
	}

	userID, err := services.ParseObjectID(userIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return primitive.NilObjectID, false
	}
	return userID, true
}

// CreateWorkflowHandler creates a new workflow
func CreateWorkflowHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	var request struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		ClusterID   string `json:"cluster_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data"})
		return
	}

	workflow := &models.Workflow{
		Name:        request.Name,
		Description: request.Description,
		OwnerID:     userID,
		ClusterID:   request.ClusterID,
	}

	if err := services.CreateWorkflow(workflow); err != nil {
		logrus.Errorf("Failed to create workflow: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create workflow"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":      workflow.ID.Hex(),
		"message": "Workflow created successfully",
		"workflow": gin.H{
			"id":          workflow.ID.Hex(),
			"name":        workflow.Name,
			"description": workflow.Description,
			"status":      workflow.Status,
			"created_at":  workflow.CreatedAt,
		},
	})
}

// GetWorkflowHandler retrieves a workflow by ID
func GetWorkflowHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	workflowID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workflow ID"})
		return
	}

	workflow, err := services.GetWorkflowByID(workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	// Check if user owns the workflow
	if workflow.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	c.JSON(http.StatusOK, workflow)
}

// UpdateWorkflowHandler updates a workflow
func UpdateWorkflowHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	workflowID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workflow ID"})
		return
	}

	// Check ownership
	workflow, err := services.GetWorkflowByID(workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	if workflow.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// Use a whitelist approach for allowed fields
	var request struct {
		Name        *string                `json:"name"`
		Description *string                `json:"description"`
		Nodes       *[]models.WorkflowNode `json:"nodes"`
		Edges       *[]models.WorkflowEdge `json:"edges"`
		Status      *string                `json:"status"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data"})
		return
	}

	// Build updates map with only provided fields
	updates := bson.M{}
	if request.Name != nil {
		updates["name"] = *request.Name
	}
	if request.Description != nil {
		updates["description"] = *request.Description
	}
	if request.Nodes != nil {
		updates["nodes"] = *request.Nodes
	}
	if request.Edges != nil {
		updates["edges"] = *request.Edges
	}
	if request.Status != nil {
		// Validate status
		status := models.WorkflowStatus(*request.Status)
		if status != models.WorkflowStatusDraft && status != models.WorkflowStatusPublished && status != models.WorkflowStatusArchived {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status"})
			return
		}
		updates["status"] = status
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No valid fields to update"})
		return
	}

	updatedWorkflow, err := services.UpdateWorkflow(workflowID, updates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update workflow"})
		return
	}

	c.JSON(http.StatusOK, updatedWorkflow)
}

// SaveWorkflowHandler saves/updates the current workflow state without creating a new version
func SaveWorkflowHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	workflowID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workflow ID"})
		return
	}

	// Check ownership
	workflow, err := services.GetWorkflowByID(workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	if workflow.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	var request struct {
		Nodes       []models.WorkflowNode `json:"nodes"`
		Edges       []models.WorkflowEdge `json:"edges"`
		Description *string               `json:"description"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data"})
		return
	}

	// Detect deleted nodes - only cleanup if workflow has been run before
	var deletedNodes []models.WorkflowNode
	if workflow.RunCount > 0 {
		// Build a map of new node IDs
		newNodeIDs := make(map[string]bool)
		for _, node := range request.Nodes {
			newNodeIDs[node.ID] = true
		}

		// Find nodes that existed before but are not in the new nodes
		for _, node := range workflow.Nodes {
			if !newNodeIDs[node.ID] {
				// Only cleanup deployment and service nodes
				if node.Type == "deployment" || node.Type == "service" {
					deletedNodes = append(deletedNodes, node)
				}
			}
		}
	}

	// Cleanup K8s resources for deleted nodes (non-blocking)
	if len(deletedNodes) > 0 {
		executor := services.NewWorkflowExecutor()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		if err := executor.CleanupDeletedNodes(ctx, workflow, deletedNodes, userID); err != nil {
			// Log the error but proceed with the save
			logrus.WithError(err).WithField("workflow_id", workflowID.Hex()).Warn("Failed to cleanup deleted nodes K8s resources")
		}
	}

	// Build updates for the current workflow
	updates := bson.M{
		"nodes": request.Nodes,
		"edges": request.Edges,
	}

	if request.Description != nil {
		updates["description"] = *request.Description
	}

	updatedWorkflow, err := services.UpdateWorkflow(workflowID, updates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save workflow"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Workflow saved successfully",
		"workflow": updatedWorkflow,
	})
}

// ListWorkflowsHandler lists all workflows for the authenticated user
func ListWorkflowsHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	workflows, err := services.ListWorkflows(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list workflows"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"workflows": workflows})
}

// DeleteWorkflowHandler deletes a workflow
func DeleteWorkflowHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	workflowID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workflow ID"})
		return
	}

	// Check ownership
	workflow, err := services.GetWorkflowByID(workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	if workflow.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	if err := services.DeleteWorkflow(workflowID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete workflow"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Workflow deleted successfully"})
}

// CloneWorkflowHandler creates a copy of an existing workflow
func CloneWorkflowHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	workflowID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workflow ID"})
		return
	}

	var request struct {
		Name string `json:"name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data"})
		return
	}

	clonedWorkflow, err := services.CloneWorkflow(workflowID, request.Name, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clone workflow"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":       clonedWorkflow.ID.Hex(),
		"message":  "Workflow cloned successfully",
		"workflow": clonedWorkflow,
	})
}

// UpdateWorkflowStatusHandler updates the status of a workflow
func UpdateWorkflowStatusHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	workflowID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workflow ID"})
		return
	}

	// Check ownership
	workflow, err := services.GetWorkflowByID(workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	if workflow.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	var request struct {
		Status string `json:"status" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data"})
		return
	}

	status := models.WorkflowStatus(request.Status)
	if status != models.WorkflowStatusDraft && status != models.WorkflowStatusPublished && status != models.WorkflowStatusArchived {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status"})
		return
	}

	// If archiving, cleanup K8s resources first
	var cleanupWarning string
	if status == models.WorkflowStatusArchived {
		executor := services.NewWorkflowExecutor()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		if err := executor.CleanupWorkflowResources(ctx, workflow, userID); err != nil {
			// Log the error but proceed with archiving
			logrus.WithError(err).WithField("workflow_id", workflowID.Hex()).Warn("Failed to cleanup K8s resources during archive")
			cleanupWarning = "K8s resources may not have been fully cleaned up: " + err.Error()
		}
	}

	if err := services.UpdateWorkflowStatus(workflowID, status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update workflow status"})
		return
	}

	response := gin.H{"message": "Workflow status updated successfully"}
	if cleanupWarning != "" {
		response["warning"] = cleanupWarning
	}
	c.JSON(http.StatusOK, response)
}

// RunWorkflowRequest contains runtime data for workflow execution
type RunWorkflowRequest struct {
	TriggerData map[string]interface{} `json:"trigger_data"`
}

// RunWorkflowHandler creates a version snapshot and runs the workflow
func RunWorkflowHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	workflowID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workflow ID"})
		return
	}

	// Parse request body for runtime data (e.g., secrets)
	var req RunWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// If no body provided, that's fine - just use empty trigger data
		req.TriggerData = make(map[string]interface{})
	}

	// Check ownership
	workflow, err := services.GetWorkflowByID(workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	if workflow.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// Check if workflow is in a runnable state
	if workflow.Status != models.WorkflowStatusPublished {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Workflow must be published to run"})
		return
	}

	// Create a version snapshot before running
	versionDescription := "Snapshot for run at " + time.Now().Format(time.RFC3339)
	version, versionErr := services.SaveWorkflowVersion(workflowID, workflow.Nodes, workflow.Edges, versionDescription, userID)
	if versionErr != nil {
		logrus.Errorf("Failed to create workflow version snapshot: %v", versionErr)
		// Continue with run even if version snapshot fails
	}

	// Execute workflow using the new executor with runtime data
	executor := services.NewWorkflowExecutor()
	workflowRun, err := executor.ExecuteWorkflow(c.Request.Context(), workflowID, userID, req.TriggerData)

	// Update version with run info (regardless of success/failure)
	if version != nil && workflowRun != nil {
		runStatus := string(workflowRun.Status)
		if updateErr := services.UpdateVersionRunStatus(version.ID, workflowRun.ID, runStatus); updateErr != nil {
			logrus.Errorf("Failed to update version run status: %v", updateErr)
		}
	}

	if err != nil {
		logrus.Errorf("Failed to execute workflow: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to execute workflow",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Workflow execution started",
		"run_id":  workflowRun.ID,
		"status":  workflowRun.Status,
		"logs":    workflowRun.Logs,
	})
}

// GetWorkflowRunsHandler gets the execution history of a workflow
func GetWorkflowRunsHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	workflowID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workflow ID"})
		return
	}

	// Check ownership
	workflow, err := services.GetWorkflowByID(workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	if workflow.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 100 {
		limit = 10
	}

	runs, err := services.GetWorkflowRuns(workflowID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get workflow runs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

// GetTemplatesHandler returns all available component templates
func GetTemplatesHandler(c *gin.Context) {
	_, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	// Get the global template registry
	registry := template.GetGlobalRegistry()
	if registry == nil {
		logrus.Error("Template registry not initialized")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Template registry not initialized"})
		return
	}

	// Get all templates
	templates := registry.GetAllTemplates()

	c.JSON(http.StatusOK, gin.H{"templates": templates})
}

// StreamWorkflowStatusHandler streams real-time workflow status updates via Server-Sent Events (SSE)
func StreamWorkflowStatusHandler(c *gin.Context) {
	userID, ok := getUserIDFromContext(c)
	if !ok {
		return
	}

	workflowID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workflow ID"})
		return
	}

	// Use request context only for initial DB query
	reqCtx := c.Request.Context()

	// Check ownership
	workflow, err := services.GetWorkflowByID(workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	if workflow.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Create a context that gets cancelled only when client disconnects
	// Use background context for the stream to avoid request timeout
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Monitor client disconnect
	go func() {
		<-reqCtx.Done()
		logrus.WithField("workflow_id", workflowID.Hex()).Info("Client disconnected from workflow status stream")
		cancel()
	}()

	// Subscribe to workflow status updates with unified broadcaster
	broadcaster := services.GetSSEBroadcaster()
	streamKey := fmt.Sprintf("workflow:%s", workflowID.Hex())
	eventChan := broadcaster.Subscribe(streamKey, 10) // Buffer size 10 for status updates
	defer broadcaster.Unsubscribe(streamKey, eventChan)

	logrus.WithField("workflow_id", workflowID.Hex()).Info("Client connected to workflow status stream")

	// Send initial workflow state as metadata
	metadataJSON, err := json.Marshal(workflow)
	if err != nil {
		logrus.WithError(err).Error("Failed to marshal workflow metadata for SSE stream")
		c.SSEvent("error", "Failed to create stream metadata")
		c.Writer.Flush()
		return
	}
	c.SSEvent("metadata", string(metadataJSON))
	c.Writer.Flush()

	// Start watchers for deployed resources if workflow has been run
	// This ensures watchers restart after backend restarts
	if workflow.RunCount > 0 {
		go startWatchersForWorkflow(workflow, userID)
	}

	// Listen for events from the broadcaster
	for {
		select {
		case <-streamCtx.Done():
			// Client disconnected
			return

		case event, ok := <-eventChan:
			if !ok {
				// Channel closed, send complete event and exit
				c.SSEvent("complete", "Stream closed")
				c.Writer.Flush()
				return
			}

			// Only process workflow events (filter out other stream types)
			if event.Type != "workflow" {
				continue
			}

			// Marshal event data to JSON (not BSON!)
			eventJSON, err := json.Marshal(event)
			if err != nil {
				logrus.WithError(err).Warn("Failed to marshal workflow status event")
				continue
			}

			logrus.WithFields(logrus.Fields{
				"workflow_id": workflowID.Hex(),
				"event_type":  event.EventType,
				"node_id":     event.Data["node_id"],
			}).Info("Sending SSE event to client")

			// Send SSE event using EventType (node_update, workflow_sync, etc.)
			c.SSEvent(event.EventType, string(eventJSON))
			c.Writer.Flush()

			// Check for write errors (client disconnected)
			if c.Errors.Last() != nil {
				logrus.WithError(c.Errors.Last()).Warn("Error writing SSE event, breaking stream")
				return
			}
		}
	}
}

// startWatchersForWorkflow starts resource watchers for all nodes in a workflow
// This is called when a client connects to the SSE stream and the workflow has been run
func startWatchersForWorkflow(workflow *models.Workflow, userID primitive.ObjectID) {
	logger := logrus.WithFields(logrus.Fields{
		"workflow_id":   workflow.ID.Hex(),
		"workflow_name": workflow.Name,
	})

	// Get K8s config for the cluster
	clusterService := services.GetKubernetesClusterService()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cluster, err := clusterService.GetClusterByName(ctx, userID, workflow.ClusterID)
	if err != nil {
		logger.WithError(err).Warn("Failed to get cluster for watchers")
		return
	}

	auth := clusterService.ClusterToAuthConfig(cluster)
	restConfig, err := auth.BuildRESTConfig()
	if err != nil {
		logger.WithError(err).Warn("Failed to build REST config for watchers")
		return
	}

	watcherManager := services.GetResourceWatcherManager()
	watchersStarted := 0

	for _, node := range workflow.Nodes {
		// Skip nodes that don't have status (not deployed)
		if node.Data == nil {
			continue
		}

		// Get resource name and namespace
		resourceName, _ := node.Data["name"].(string)
		namespace, _ := node.Data["namespace"].(string)
		if namespace == "" {
			namespace = "default"
		}

		if resourceName == "" {
			continue
		}

		// Only watch supported resource types
		resourceType := node.Type
		if resourceType != "deployment" && resourceType != "service" &&
		   resourceType != "statefulset" && resourceType != "daemonset" &&
		   resourceType != "job" && resourceType != "pod" {
			continue
		}

		// Start watcher (manager handles deduplication)
		err := watcherManager.StartWatcher(workflow.ID, node.ID, resourceName, namespace, resourceType, restConfig)
		if err != nil {
			logger.WithError(err).WithFields(logrus.Fields{
				"node_id":       node.ID,
				"resource_type": resourceType,
				"resource_name": resourceName,
			}).Warn("Failed to start watcher for node")
		} else {
			watchersStarted++
		}
	}

	if watchersStarted > 0 {
		logger.WithField("watchers_started", watchersStarted).Info("Started watchers for workflow nodes")
	}
}