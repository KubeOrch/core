package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/KubeOrch/core/middleware"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	corev1 "k8s.io/api/core/v1"
)

type ResourcesHandler struct {
	resourceService *services.ResourceService
	clusterService  *services.KubernetesClusterService
	logger          *logrus.Logger
}

func NewResourcesHandler() *ResourcesHandler {
	return &ResourcesHandler{
		resourceService: services.GetResourceService(),
		clusterService:  services.GetKubernetesClusterService(),
		logger:          logrus.New(),
	}
}

// GetResources retrieves resources from database (with optional sync)
func (h *ResourcesHandler) GetResources(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	// Query parameters
	clusterName := c.Query("cluster")
	namespace := c.Query("namespace")
	resourceType := c.Query("type")
	syncFirst := c.Query("sync") == "true"

	ctx := c.Request.Context()

	// If sync requested, trigger async sync
	if syncFirst {
		if clusterName != "" && clusterName != "all" {
			// Sync specific cluster
			cluster, err := h.clusterService.GetClusterByName(ctx, userID, clusterName)
			if err == nil {
				go func() {
					if err := h.resourceService.SyncClusterResources(context.Background(), userID, cluster); err != nil {
						h.logger.WithError(err).Errorf("Failed to sync cluster %s", clusterName)
					}
				}()
			}
		} else {
			// Sync all clusters
			clusters, err := h.clusterService.ListUserClusters(ctx, userID)
			if err == nil {
				go func() {
					for _, cluster := range clusters {
						if err := h.resourceService.SyncClusterResources(context.Background(), userID, cluster); err != nil {
							h.logger.WithError(err).Errorf("Failed to sync cluster %s", cluster.Name)
						}
					}
				}()
			}
		}
	}

	// Build filter
	filter := bson.M{}
	if clusterName != "" && clusterName != "all" {
		filter["clusterName"] = clusterName
	}
	if namespace != "" && namespace != "all" {
		filter["namespace"] = namespace
	}
	if resourceType != "" && resourceType != "all" {
		filter["type"] = resourceType
	}

	// Get resources from database
	resources, err := h.resourceService.GetResources(ctx, userID, filter)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get resources")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get resources"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"resources": resources,
		"count":     len(resources),
	})
}

// GetResourceByID retrieves a single resource
func (h *ResourcesHandler) GetResourceByID(c *gin.Context) {
	resourceID := c.Param("id")

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	objID, err := primitive.ObjectIDFromHex(resourceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid resource ID"})
		return
	}

	ctx := c.Request.Context()
	resource, err := h.resourceService.GetResourceByID(ctx, objID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Resource not found"})
		return
	}

	// Get resource history
	history, _ := h.resourceService.GetResourceHistory(ctx, objID)

	c.JSON(http.StatusOK, gin.H{
		"resource": resource,
		"history":  history,
	})
}

// UpdateResourceUserFields updates user-specific fields (tags, notes, favorites)
func (h *ResourcesHandler) UpdateResourceUserFields(c *gin.Context) {
	resourceID := c.Param("id")

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	objID, err := primitive.ObjectIDFromHex(resourceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid resource ID"})
		return
	}

	var updates struct {
		UserTags   []string `json:"userTags"`
		UserNotes  string   `json:"userNotes"`
		IsFavorite bool     `json:"isFavorite"`
	}

	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	ctx := c.Request.Context()
	updateDoc := bson.M{
		"userTags":   updates.UserTags,
		"userNotes":  updates.UserNotes,
		"isFavorite": updates.IsFavorite,
	}

	if err := h.resourceService.UpdateResourceUserFields(ctx, objID, userID, updateDoc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update resource"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Resource updated successfully"})
}

// SyncResources triggers a manual sync of all clusters
func (h *ResourcesHandler) SyncResources(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	clusters, err := h.clusterService.ListUserClusters(ctx, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get clusters"})
		return
	}

	// Start sync for all clusters in background
	go func() {
		for _, cluster := range clusters {
			if err := h.resourceService.SyncClusterResources(context.Background(), userID, cluster); err != nil {
				h.logger.WithError(err).Errorf("Failed to sync cluster %s", cluster.Name)
			}
		}
	}()

	c.JSON(http.StatusAccepted, gin.H{"message": "Sync started for all clusters"})
}

// GetPodLogs fetches logs for a specific pod
func (h *ResourcesHandler) GetPodLogs(c *gin.Context) {
	resourceID := c.Param("id")
	container := c.Query("container")
	tailLines := c.DefaultQuery("tail", "1000")
	previous := c.Query("previous") == "true"

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	objID, err := primitive.ObjectIDFromHex(resourceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid resource ID"})
		return
	}

	ctx := c.Request.Context()

	// Get the resource from database
	resource, err := h.resourceService.GetResourceByID(ctx, objID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Resource not found"})
		return
	}

	// Verify it's a pod
	if resource.Type != "Pod" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Resource is not a pod"})
		return
	}

	// Get the cluster connection
	cluster, err := h.clusterService.GetClusterByName(ctx, userID, resource.ClusterName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Cluster not found"})
		return
	}

	clientset, err := h.clusterService.CreateClusterConnection(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to cluster"})
		return
	}

	// Convert tailLines to int64
	var tailLinesInt64 int64 = 1000
	fmt.Sscanf(tailLines, "%d", &tailLinesInt64)

	// If no container specified and pod has containers, use the first one
	if container == "" && len(resource.Spec.Containers) > 0 {
		container = resource.Spec.Containers[0].Name
	}

	podLogOptions := &corev1.PodLogOptions{
		Container: container,
		TailLines: &tailLinesInt64,
		Previous:  previous,
	}

	req := clientset.CoreV1().Pods(resource.Namespace).GetLogs(resource.Name, podLogOptions)
	logs, err := req.DoRaw(ctx)
	if err != nil {
		h.logger.WithError(err).Error("Failed to fetch pod logs")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch logs"})
		return
	}

	// Access logging can be added here if needed

	c.JSON(http.StatusOK, gin.H{
		"logs":      string(logs),
		"pod":       resource.Name,
		"container": container,
		"namespace": resource.Namespace,
		"cluster":   resource.ClusterName,
	})
}

// GetDeploymentPods gets all pods belonging to a deployment
func (h *ResourcesHandler) GetDeploymentPods(c *gin.Context) {
	resourceID := c.Param("id")

	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	objID, err := primitive.ObjectIDFromHex(resourceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid resource ID"})
		return
	}

	ctx := c.Request.Context()

	// Get the deployment resource
	deployment, err := h.resourceService.GetResourceByID(ctx, objID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Resource not found"})
		return
	}

	// Verify it's a deployment or statefulset
	if deployment.Type != "Deployment" && deployment.Type != "StatefulSet" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Resource is not a deployment or statefulset"})
		return
	}

	// Find pods that belong to this deployment
	filter := bson.M{
		"clusterName": deployment.ClusterName,
		"namespace":   deployment.Namespace,
		"type":        "Pod",
		"ownerReferences": bson.M{
			"$elemMatch": bson.M{
				"name": deployment.Name,
				"kind": string(deployment.Type),
			},
		},
	}

	pods, err := h.resourceService.GetResources(ctx, userID, filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get pods"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deployment": deployment,
		"pods":       pods,
		"count":      len(pods),
	})
}