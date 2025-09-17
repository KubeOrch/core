package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/KubeOrch/core/models"
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

	if err := services.UpdateWorkflowStatus(workflowID, status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update workflow status"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Workflow status updated successfully"})
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
	if err := services.SaveWorkflowVersion(workflowID, workflow.Nodes, workflow.Edges, versionDescription, userID); err != nil {
		logrus.Errorf("Failed to create workflow version snapshot: %v", err)
		// Continue with run even if version snapshot fails
	}

	// Execute workflow using the new executor
	executor := services.NewWorkflowExecutor()
	workflowRun, err := executor.ExecuteWorkflow(c.Request.Context(), workflowID, userID)
	if err != nil {
		logrus.Errorf("Failed to execute workflow: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to execute workflow",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Workflow execution started",
		"run_id": workflowRun.ID,
		"status": workflowRun.Status,
		"logs": workflowRun.Logs,
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