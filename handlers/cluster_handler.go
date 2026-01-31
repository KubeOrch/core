package handlers

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/KubeOrch/core/middleware"
	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ClusterHandler struct {
	service *services.KubernetesClusterService
	logger  *logrus.Logger
}

func NewClusterHandler() *ClusterHandler {
	return &ClusterHandler{
		service: services.GetKubernetesClusterService(),
		logger:  logrus.New(),
	}
}

type AddClusterRequest struct {
	Name        string                    `json:"name" binding:"required"`
	DisplayName string                    `json:"displayName"`
	Description string                    `json:"description"`
	Server      string                    `json:"server" binding:"required"`
	AuthType    models.ClusterAuthType    `json:"authType" binding:"required"`
	Credentials models.ClusterCredentials `json:"credentials" binding:"required"`
	SingleNode  bool                      `json:"singleNode"`
	Labels      map[string]string         `json:"labels,omitempty"`
}

type ClusterResponse struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	DisplayName string                 `json:"displayName"`
	Description string                 `json:"description"`
	Server      string                 `json:"server"`
	AuthType    models.ClusterAuthType `json:"authType"`
	Status      models.ClusterStatus   `json:"status"`
	Default     bool                   `json:"default"`
	SingleNode  bool                   `json:"singleNode"`
	Labels      map[string]string      `json:"labels,omitempty"`
	Metadata    models.ClusterMetadata `json:"metadata,omitempty"`
}

func (h *ClusterHandler) AddCluster(c *gin.Context) {
	var req AddClusterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.WithError(err).Error("Invalid request body")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	cluster := &models.Cluster{
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Description: req.Description,
		Server:      req.Server,
		AuthType:    req.AuthType,
		Credentials: req.Credentials,
		SingleNode:  req.SingleNode,
		Labels:      req.Labels,
	}

	ctx := c.Request.Context()
	if err := h.service.AddCluster(ctx, userID, cluster); err != nil {
		h.logger.WithError(err).Error("Failed to add cluster")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Cluster added successfully",
		"cluster": clusterToResponse(cluster),
	})
}

func (h *ClusterHandler) ListClusters(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	clusters, err := h.service.ListUserClusters(ctx, userID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list clusters")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get default cluster
	defaultCluster, err := h.service.GetDefaultCluster(ctx, userID)
	if err != nil {
		// A default cluster may not exist, which is not an error
		// We should only fail on actual errors
		h.logger.WithError(err).Warn("Failed to get default cluster")
	}
	defaultID := ""
	if defaultCluster != nil {
		defaultID = defaultCluster.ID.Hex()
	}

	response := make([]ClusterResponse, 0, len(clusters))
	for _, cluster := range clusters {
		clusterResp := clusterToResponse(cluster)
		clusterResp.Default = cluster.ID.Hex() == defaultID
		response = append(response, clusterResp)
	}

	c.JSON(http.StatusOK, gin.H{
		"clusters": response,
		"default":  defaultID,
	})
}

func (h *ClusterHandler) GetCluster(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cluster name is required"})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	cluster, err := h.service.GetClusterByName(ctx, userID, name)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get cluster")
		c.JSON(http.StatusNotFound, gin.H{"error": "Cluster not found"})
		return
	}

	c.JSON(http.StatusOK, clusterToResponse(cluster))
}

func (h *ClusterHandler) RemoveCluster(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cluster name is required"})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	if err := h.service.RemoveCluster(ctx, userID, name); err != nil {
		h.logger.WithError(err).Error("Failed to remove cluster")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Cluster removed successfully",
		"cluster": name,
	})
}

func (h *ClusterHandler) SetDefaultCluster(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cluster name is required"})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	if err := h.service.SetDefaultCluster(ctx, userID, name); err != nil {
		h.logger.WithError(err).Error("Failed to set default cluster")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Default cluster set successfully",
		"cluster": name,
	})
}

func (h *ClusterHandler) GetDefaultCluster(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	cluster, err := h.service.GetDefaultCluster(ctx, userID)
	if err != nil || cluster == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No default cluster set"})
		return
	}

	response := clusterToResponse(cluster)
	response.Default = true

	c.JSON(http.StatusOK, response)
}

func (h *ClusterHandler) TestConnection(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cluster name is required"})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	if err := h.service.TestClusterConnection(ctx, userID, name); err != nil {
		h.logger.WithError(err).Error("Cluster connection test failed")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   err.Error(),
			"cluster": name,
			"status":  "failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Connection test successful",
		"cluster": name,
		"status":  "connected",
	})
}

func (h *ClusterHandler) RefreshMetadata(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cluster name is required"})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	if err := h.service.RefreshClusterMetadata(ctx, userID, name); err != nil {
		h.logger.WithError(err).Error("Failed to refresh cluster metadata")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get updated cluster
	cluster, err := h.service.GetClusterByName(ctx, userID, name)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get updated cluster info after metadata refresh")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve updated cluster information"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Metadata refreshed successfully",
		"metadata": cluster.Metadata,
	})
}

func (h *ClusterHandler) GetClusterStatus(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cluster name is required"})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	cluster, err := h.service.GetClusterByName(ctx, userID, name)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get cluster")
		c.JSON(http.StatusNotFound, gin.H{"error": "Cluster not found"})
		return
	}

	// Check if status is stale (not checked in last 2 minutes)
	isStale := time.Since(cluster.LastCheck) > 2*time.Minute
	
	c.JSON(http.StatusOK, gin.H{
		"cluster":   name,
		"status":    cluster.Status,
		"lastCheck": cluster.LastCheck,
		"isStale":   isStale,
		"isOnline":  cluster.Status == models.ClusterStatusConnected,
	})
}

func (h *ClusterHandler) GetClusterLogs(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cluster name is required"})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	limit := int64(100)
	if l := c.Query("limit"); l != "" {
		if parsedLimit, err := strconv.ParseInt(l, 10, 64); err == nil {
			if parsedLimit > 0 && parsedLimit <= 1000 { // Cap the limit to a max of 1000
				limit = parsedLimit
			}
		}
	}

	ctx := c.Request.Context()
	logs, err := h.service.GetClusterLogs(ctx, userID, name, limit)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get cluster logs")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster": name,
		"logs":    logs,
	})
}

type UpdateClusterRequest struct {
	DisplayName string                    `json:"displayName"`
	Description string                    `json:"description"`
	Server      string                    `json:"server"`
	AuthType    models.ClusterAuthType    `json:"authType"`
	Credentials *models.ClusterCredentials `json:"credentials,omitempty"` // Optional - only if updating token
	SingleNode  *bool                     `json:"singleNode,omitempty"`
	Insecure    *bool                     `json:"insecure,omitempty"`
	Labels      map[string]string         `json:"labels,omitempty"`
}

func (h *ClusterHandler) UpdateCluster(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cluster name is required"})
		return
	}

	var req UpdateClusterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.WithError(err).Error("Invalid request body")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()

	// Get existing cluster
	cluster, err := h.service.GetClusterByName(ctx, userID, name)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get cluster")
		c.JSON(http.StatusNotFound, gin.H{"error": "Cluster not found"})
		return
	}

	// Update fields
	if req.DisplayName != "" {
		cluster.DisplayName = req.DisplayName
	}
	if req.Description != "" {
		cluster.Description = req.Description
	}
	if req.Server != "" {
		cluster.Server = req.Server
	}
	if req.AuthType != "" {
		cluster.AuthType = req.AuthType
	}
	if req.Labels != nil {
		cluster.Labels = req.Labels
	}

	// Only update credentials if provided
	if req.Credentials != nil {
		cluster.Credentials = *req.Credentials
	}

	// Update single node mode if provided
	if req.SingleNode != nil {
		cluster.SingleNode = *req.SingleNode
	}

	// Update insecure mode if provided
	// WARNING: Enabling insecure mode skips TLS certificate verification
	// This is blocked in production environments (GIN_MODE=release) for security
	if req.Insecure != nil && *req.Insecure {
		ginMode := os.Getenv("GIN_MODE")
		if ginMode == "release" {
			h.logger.WithFields(logrus.Fields{
				"cluster": name,
				"user_id": userID.Hex(),
			}).Warn("Attempted to enable insecure mode in production environment - request denied")
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Insecure mode cannot be enabled in production environments. TLS certificate verification is required for security.",
			})
			return
		}
		cluster.Credentials.Insecure = true
		h.logger.WithFields(logrus.Fields{
			"cluster": name,
			"user_id": userID.Hex(),
		}).Warn("Insecure mode enabled - TLS certificate verification will be skipped. This is only allowed in development/testing environments.")
	} else if req.Insecure != nil {
		cluster.Credentials.Insecure = *req.Insecure
	}

	// Update the cluster (including single-node mode changes if any)
	if err := h.service.UpdateCluster(ctx, userID, name, cluster); err != nil {
		h.logger.WithError(err).Error("Failed to update cluster")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Cluster updated successfully",
		"cluster": clusterToResponse(cluster),
	})
}

type UpdateCredentialsRequest struct {
	Credentials models.ClusterCredentials `json:"credentials" binding:"required"`
}

func (h *ClusterHandler) UpdateCredentials(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cluster name is required"})
		return
	}

	var req UpdateCredentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	if err := h.service.UpdateClusterCredentials(ctx, userID, name, req.Credentials); err != nil {
		h.logger.WithError(err).Error("Failed to update credentials")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Credentials updated successfully",
		"cluster": name,
	})
}

type ShareClusterRequest struct {
	TargetUserID string   `json:"targetUserId" binding:"required"`
	Role         string   `json:"role" binding:"required"`
	Namespaces   []string `json:"namespaces,omitempty"`
}

func (h *ClusterHandler) ShareCluster(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cluster name is required"})
		return
	}

	var req ShareClusterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	targetID, err := primitive.ObjectIDFromHex(req.TargetUserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid target user ID"})
		return
	}

	ctx := c.Request.Context()
	cluster, err := h.service.GetClusterByName(ctx, userID, name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Cluster not found"})
		return
	}

	if err := h.service.ShareCluster(ctx, userID, cluster.ID, targetID, req.Role, req.Namespaces); err != nil {
		h.logger.WithError(err).Error("Failed to share cluster")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Cluster shared successfully",
		"cluster": name,
	})
}

func (h *ClusterHandler) GetClusterMetrics(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cluster name is required"})
		return
	}

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	metrics, err := h.service.GetClusterMetrics(ctx, userID, name)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get cluster metrics")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, metrics)
}

// Helper function to convert cluster model to response
func clusterToResponse(cluster *models.Cluster) ClusterResponse {
	return ClusterResponse{
		ID:          cluster.ID.Hex(),
		Name:        cluster.Name,
		DisplayName: cluster.DisplayName,
		Description: cluster.Description,
		Server:      cluster.Server,
		AuthType:    cluster.AuthType,
		Status:      cluster.Status,
		Default:     cluster.Default,
		SingleNode:  cluster.SingleNode,
		Labels:      cluster.Labels,
		Metadata:    cluster.Metadata,
	}
}
