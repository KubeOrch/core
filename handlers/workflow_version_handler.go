package handlers

import (
	"net/http"
	"strconv"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// getWorkflowFromContext retrieves the workflow set by WorkflowOwnershipMiddleware
func getWorkflowFromContext(c *gin.Context) (*models.Workflow, primitive.ObjectID, bool) {
	workflow, exists := c.Get("workflow")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Workflow not found in context"})
		return nil, primitive.NilObjectID, false
	}

	ownerID, exists := c.Get("ownerID")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Owner ID not found in context"})
		return nil, primitive.NilObjectID, false
	}

	return workflow.(*models.Workflow), ownerID.(primitive.ObjectID), true
}

// ListVersionsHandler lists all versions for a workflow with pagination
func ListVersionsHandler(c *gin.Context) {
	workflow, _, ok := getWorkflowFromContext(c)
	if !ok {
		return
	}

	// Parse pagination params
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 50 {
		limit = 10
	}

	response, err := services.GetVersions(workflow.ID, page, limit)
	if err != nil {
		logrus.WithError(err).Error("Failed to get workflow versions")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get versions"})
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetVersionHandler retrieves a specific version by version number
func GetVersionHandler(c *gin.Context) {
	workflow, _, ok := getWorkflowFromContext(c)
	if !ok {
		return
	}

	versionNum, err := strconv.Atoi(c.Param("version"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid version number"})
		return
	}

	version, err := services.GetVersionByNumber(workflow.ID, versionNum)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Version not found"})
		return
	}

	c.JSON(http.StatusOK, version)
}

// CreateVersionHandler creates a new version manually
func CreateVersionHandler(c *gin.Context) {
	workflow, userID, ok := getWorkflowFromContext(c)
	if !ok {
		return
	}

	var request struct {
		Name        string `json:"name"`
		Tag         string `json:"tag"`
		Description string `json:"description"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data"})
		return
	}

	input := services.CreateVersionInput{
		WorkflowID:  workflow.ID,
		Nodes:       workflow.Nodes,
		Edges:       workflow.Edges,
		Description: request.Description,
		UserID:      userID,
		IsAutomatic: false,
		Name:        request.Name,
		Tag:         request.Tag,
	}

	version, err := services.CreateVersion(input)
	if err != nil {
		logrus.WithError(err).Error("Failed to create workflow version")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create version"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Version created successfully",
		"version": version,
	})
}

// UpdateVersionHandler updates version metadata (name, tag, description)
func UpdateVersionHandler(c *gin.Context) {
	workflow, _, ok := getWorkflowFromContext(c)
	if !ok {
		return
	}

	versionNum, err := strconv.Atoi(c.Param("version"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid version number"})
		return
	}

	var request struct {
		Name        *string `json:"name"`
		Tag         *string `json:"tag"`
		Description *string `json:"description"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request data"})
		return
	}

	input := services.UpdateVersionMetadataInput{
		Name:        request.Name,
		Tag:         request.Tag,
		Description: request.Description,
	}

	version, err := services.UpdateVersionMetadata(workflow.ID, versionNum, input)
	if err != nil {
		if err.Error() == "version not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Version not found"})
			return
		}
		logrus.WithError(err).Error("Failed to update workflow version")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update version"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Version updated successfully",
		"version": version,
	})
}

// RestoreVersionHandler restores a previous version
func RestoreVersionHandler(c *gin.Context) {
	workflow, userID, ok := getWorkflowFromContext(c)
	if !ok {
		return
	}

	versionNum, err := strconv.Atoi(c.Param("version"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid version number"})
		return
	}

	version, err := services.RestoreVersion(workflow.ID, versionNum, userID)
	if err != nil {
		if err.Error() == "version not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Version not found"})
			return
		}
		logrus.WithError(err).Error("Failed to restore workflow version")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to restore version"})
		return
	}

	// Get the updated workflow to return
	updatedWorkflow, err := services.GetWorkflowByID(workflow.ID)
	if err != nil {
		logrus.WithError(err).Warn("Failed to get updated workflow after restore")
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Version restored successfully",
		"version":  version,
		"workflow": updatedWorkflow,
	})
}

// CompareVersionsHandler compares two versions and returns the differences
func CompareVersionsHandler(c *gin.Context) {
	workflow, _, ok := getWorkflowFromContext(c)
	if !ok {
		return
	}

	v1, err := strconv.Atoi(c.Query("v1"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or missing version number v1"})
		return
	}

	v2, err := strconv.Atoi(c.Query("v2"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or missing version number v2"})
		return
	}

	diff, err := services.CompareVersions(workflow.ID, v1, v2)
	if err != nil {
		if err.Error() == "version not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "One or both versions not found"})
			return
		}
		logrus.WithError(err).Error("Failed to compare workflow versions")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to compare versions"})
		return
	}

	c.JSON(http.StatusOK, diff)
}
