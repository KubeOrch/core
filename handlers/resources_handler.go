package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/KubeOrch/core/middleware"
	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
)

type ResourcesHandler struct {
	resourceService *services.ResourceService
	clusterService  *services.KubernetesClusterService
	logger          *logrus.Logger
}

// ResourceSummaryResponse represents the minimal resource data for list views
type ResourceSummaryResponse struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Namespace   string                 `json:"namespace"`
	Type        string                 `json:"type"`
	ClusterName string                 `json:"clusterName"`
	Status      string                 `json:"status"`
	CreatedAt   time.Time              `json:"createdAt"`
	IsFavorite  bool                   `json:"isFavorite"`
	Summary     map[string]interface{} `json:"summary"`
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
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer cancel()
					if err := h.resourceService.SyncClusterResources(ctx, userID, cluster); err != nil {
						h.logger.WithError(err).Errorf("Failed to sync cluster %s", clusterName)
					}
				}()
			}
		} else {
			// Sync all clusters
			clusters, err := h.clusterService.ListUserClusters(ctx, userID)
			if err == nil {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer cancel()
					for _, cluster := range clusters {
						if err := h.resourceService.SyncClusterResources(ctx, userID, cluster); err != nil {
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

	// Return minimal list for table view (optimization)
	minimalList := make([]ResourceSummaryResponse, len(resources))
	for i, r := range resources {
		summary := buildResourceSummary(r)
		minimalList[i] = ResourceSummaryResponse{
			ID:          r.ID.Hex(),
			Name:        r.Name,
			Namespace:   r.Namespace,
			Type:        string(r.Type),
			ClusterName: r.ClusterName,
			Status:      string(r.Status),
			CreatedAt:   r.CreatedAt,
			IsFavorite:  r.IsFavorite,
			Summary:     summary,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"resources": minimalList,
		"count":     len(minimalList),
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
	history, err := h.resourceService.GetResourceHistory(ctx, objID)
	if err != nil {
		h.logger.WithError(err).Warnf("Failed to get history for resource %s", resourceID)
	}

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
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		for _, cluster := range clusters {
			if err := h.resourceService.SyncClusterResources(ctx, userID, cluster); err != nil {
				h.logger.WithError(err).Errorf("Failed to sync cluster %s", cluster.Name)
			}
		}
	}()

	c.JSON(http.StatusAccepted, gin.H{"message": "Sync started for all clusters"})
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

	// Find pods that belong to this deployment/statefulset
	// In Kubernetes, Deployment -> ReplicaSet -> Pod, so pods don't directly reference the deployment.
	// Instead, we match by the "app" label which is typically set to the deployment name.
	// We also match pods whose name starts with the deployment name (deployment-X-hash-hash pattern).
	filter := bson.M{
		"clusterName": deployment.ClusterName,
		"namespace":   deployment.Namespace,
		"type":        "Pod",
		"$or": []bson.M{
			// Match by "app" label (most reliable)
			{"labels.app": deployment.Name},
			// Match by name prefix (fallback for pods without app label)
			{"name": bson.M{"$regex": "^" + deployment.Name + "-"}},
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

// StreamPodLogs streams pod logs via Server-Sent Events (SSE)
// First sends historical logs, then streams live logs
func (h *ResourcesHandler) StreamPodLogs(c *gin.Context) {
	resourceID := c.Param("id")
	container := c.Query("container")
	follow := c.DefaultQuery("follow", "true") == "true"
	tailLines := c.DefaultQuery("tail", "100")
	sinceSeconds := c.Query("sinceSeconds") // Optional: logs from last N seconds

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

	// Use request context only for initial DB queries
	reqCtx := c.Request.Context()

	// Get the resource from database
	resource, err := h.resourceService.GetResourceByID(reqCtx, objID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Resource not found"})
		return
	}

	// Verify it's a pod
	if resource.Type != models.ResourceTypePod {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only pods have logs. This resource is not a pod."})
		return
	}

	// Get the cluster connection
	cluster, err := h.clusterService.GetClusterByName(reqCtx, userID, resource.ClusterName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Cluster not found"})
		return
	}

	// Create a special clientset for streaming with no timeout
	// The default CreateClusterConnection sets a 5-second timeout which breaks streaming
	clientset, err := h.clusterService.CreateStreamingClusterConnection(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to cluster for streaming"})
		return
	}

	// Convert tailLines to int64 and validate range (50-5000)
	tailLinesInt64, err := strconv.ParseInt(tailLines, 10, 64)
	if err != nil {
		tailLinesInt64 = 100
	}

	// Clamp the value to the supported range to prevent abuse
	const maxTailLines = 5000
	const minTailLines = 50
	if tailLinesInt64 > maxTailLines {
		tailLinesInt64 = maxTailLines
	} else if tailLinesInt64 < minTailLines {
		tailLinesInt64 = minTailLines
	}

	// If no container specified and pod has containers, use the first one
	if container == "" && len(resource.Spec.Containers) > 0 {
		container = resource.Spec.Containers[0].Name
	}

	// Build log options
	podLogOptions := &corev1.PodLogOptions{
		Container:  container,
		Follow:     follow,
		Timestamps: true,
		TailLines:  &tailLinesInt64,
	}

	// If sinceSeconds is provided, use it instead of tailLines
	if sinceSeconds != "" {
		sinceSecondsInt64, err := strconv.ParseInt(sinceSeconds, 10, 64)
		if err == nil && sinceSecondsInt64 > 0 {
			podLogOptions.SinceSeconds = &sinceSecondsInt64
			podLogOptions.TailLines = nil // Can't use both
		}
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
		<-c.Request.Context().Done()
		h.logger.WithField("resource_id", resourceID).Info("Client disconnected from log stream")
		cancel()
	}()

	// Send initial metadata
	metadataJSON, err := json.Marshal(map[string]string{
		"pod":       resource.Name,
		"container": container,
		"namespace": resource.Namespace,
		"cluster":   resource.ClusterName,
	})
	if err != nil {
		h.logger.WithError(err).Error("Failed to marshal metadata for SSE stream")
		c.SSEvent("error", "Failed to create stream metadata")
		c.Writer.Flush()
		return
	}
	c.SSEvent("metadata", string(metadataJSON))
	c.Writer.Flush()

	// Record access
	if err := h.resourceService.RecordResourceAccess(reqCtx, objID, userID, "stream_logs", map[string]string{
		"container": container,
	}); err != nil {
		h.logger.WithError(err).Warn("Failed to record access log")
	}

	h.logger.WithField("resource_id", resourceID).Info("Client connected to pod log stream")

	// STEP 1: Fetch historical logs (if follow is true, we need both history + live stream)
	// Each subscriber gets their own historical logs independently
	if follow {
		h.logger.WithFields(map[string]interface{}{
			"resource_id": resourceID,
			"tail_lines":  tailLinesInt64,
		}).Info("Fetching historical logs for subscriber")

		// Fetch historical logs (follow=false, with tailLines)
		historicalOptions := &corev1.PodLogOptions{
			Container:  container,
			Follow:     false, // Don't follow for historical logs
			Timestamps: true,
			TailLines:  &tailLinesInt64,
		}

		histReq := clientset.CoreV1().Pods(resource.Namespace).GetLogs(resource.Name, historicalOptions)
		histStream, err := histReq.Stream(streamCtx)
		if err != nil {
			h.logger.WithError(err).Warn("Failed to fetch historical logs, continuing with live stream")
		} else {
			// Send historical logs to this subscriber only
			scanner := bufio.NewScanner(histStream)
			for scanner.Scan() {
				line := scanner.Text()
				c.SSEvent("log", line)
				c.Writer.Flush()

				// Check if client disconnected while sending history
				if c.Errors.Last() != nil {
					_ = histStream.Close()
					return
				}
			}
			_ = histStream.Close()

			if err := scanner.Err(); err != nil && err != io.EOF {
				h.logger.WithError(err).Warn("Error reading historical logs")
			}

			h.logger.WithField("resource_id", resourceID).Info("Historical logs sent to subscriber")
		}
	}

	// STEP 2: Subscribe to unified broadcaster for real-time logs
	broadcaster := services.GetSSEBroadcaster()
	streamKey := fmt.Sprintf("pod-logs:%s", resourceID)
	eventChan := broadcaster.Subscribe(streamKey, 100) // Buffer size 100 for logs
	defer broadcaster.Unsubscribe(streamKey, eventChan)

	// STEP 3: Start K8s follow stream via manager (if not already running)
	// Follow stream should NOT include tail lines - only new logs from now onwards
	logStreamManager := services.GetPodLogStreamManager()
	followOptions := &corev1.PodLogOptions{
		Container:  container,
		Follow:     true,  // Follow for new logs
		Timestamps: true,
		TailLines:  nil,   // No tail lines - we already sent historical logs above
	}

	started, err := logStreamManager.StartLogStream(resourceID, clientset, resource.Namespace, resource.Name, container, followOptions)
	if err != nil {
		h.logger.WithError(err).Error("Failed to start follow log stream")
		c.SSEvent("error", fmt.Sprintf("Failed to start follow stream: %v", err))
		c.Writer.Flush()
		return
	}

	if started {
		h.logger.WithField("resource_id", resourceID).Info("Started new K8s follow stream")
	} else {
		h.logger.WithField("resource_id", resourceID).Info("Joined existing K8s follow stream")
	}

	// Relay events from broadcaster to SSE
	for {
		select {
		case <-streamCtx.Done():
			// Client disconnected
			h.logger.WithField("resource_id", resourceID).Info("Client disconnected, checking for cleanup")
			// Check if we should stop the K8s stream (no more subscribers)
			if broadcaster.GetSubscriberCount(streamKey) == 0 {
				logStreamManager.StopLogStream(resourceID)
			}
			return

		case event, ok := <-eventChan:
			if !ok {
				// Channel closed
				c.SSEvent("complete", "Stream closed")
				c.Writer.Flush()
				return
			}

			// Only process pod-logs events
			if event.Type != "pod-logs" {
				continue
			}

			// Handle different event types
			switch event.EventType {
			case "log":
				// Extract log line from event data
				if line, ok := event.Data["line"].(string); ok {
					c.SSEvent("log", line)
				}
			case "error":
				// Extract error message
				if msg, ok := event.Data["message"].(string); ok {
					c.SSEvent("error", msg)
				}
			case "complete":
				// Stream completed
				if msg, ok := event.Data["message"].(string); ok {
					c.SSEvent("complete", msg)
				} else {
					c.SSEvent("complete", "Log stream completed")
				}
				c.Writer.Flush()
				return
			}

			c.Writer.Flush()

			// Check for write errors (client disconnected)
			if c.Errors.Last() != nil {
				h.logger.WithError(c.Errors.Last()).Warn("Error writing SSE event, breaking stream")
				return
			}
		}
	}
}

// WebSocket upgrader configuration
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// In debug mode, allow any origin for development
		if gin.Mode() == gin.DebugMode {
			return true
		}
		// In production, validate origin against allowed list
		// TODO: Load allowed origins from configuration
		origin := r.Header.Get("Origin")
		// For now, deny all in production mode to be secure by default
		// Configure allowed origins in production environment
		logrus.WithField("origin", origin).Warn("WebSocket origin validation: denying by default in production mode")
		return false
	},
}

// TerminalMessage represents WebSocket messages for terminal communication
type TerminalMessage struct {
	Type string `json:"type"` // "input", "output", "resize", "error", "close"
	Data string `json:"data"`
	Rows uint16 `json:"rows,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
}

// TerminalSession handles the bidirectional terminal stream
type TerminalSession struct {
	ws        *websocket.Conn
	sizeChan  chan remotecommand.TerminalSize
	inputChan chan []byte
	logger    *logrus.Logger
	mu        sync.Mutex
}

// Read reads from WebSocket and writes to stdin
func (t *TerminalSession) Read(p []byte) (int, error) {
	// Read from the input channel instead of directly from WebSocket
	// This prevents race conditions with the WebSocket reader goroutine
	data, ok := <-t.inputChan
	if !ok {
		// Channel closed, WebSocket connection ended
		return 0, io.EOF
	}

	// Copy data to the provided buffer
	n := copy(p, data)

	// Log the input for debugging
	t.logger.WithFields(map[string]interface{}{
		"data_len": len(data),
		"copied": n,
	}).Debug("Read input from channel")

	return n, nil
}

// Write writes output from stdout/stderr to WebSocket
func (t *TerminalSession) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	msg := TerminalMessage{
		Type: "output",
		Data: string(p),
	}

	if err := t.ws.WriteJSON(msg); err != nil {
		return 0, err
	}

	return len(p), nil
}

// Next handles terminal resize events
func (t *TerminalSession) Next() *remotecommand.TerminalSize {
	size := <-t.sizeChan
	return &size
}

// HandleTerminalSession manages the WebSocket terminal connection
func (h *ResourcesHandler) HandleTerminalSession(c *gin.Context) {
	// Extract resource ID and parameters
	resourceID := c.Param("id")
	container := c.Query("container")
	shell := c.DefaultQuery("shell", "/bin/sh") // Default to sh, can be /bin/bash

	// Validate shell parameter against allowed list
	allowedShells := []string{"/bin/sh", "/bin/bash", "sh", "bash"}
	isAllowed := false
	for _, s := range allowedShells {
		if shell == s {
			isAllowed = true
			break
		}
	}
	if !isAllowed {
		h.logger.WithField("shell", shell).Warn("Invalid shell specified")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid shell specified"})
		return
	}

	h.logger.WithFields(map[string]interface{}{
		"resourceID": resourceID,
		"container":  container,
		"shell":      shell,
		"path":       c.Request.URL.Path,
	}).Info("Terminal session request received")

	// Get authenticated user ID
	userID, err := middleware.GetUserID(c)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get user ID")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Convert resource ID to ObjectID
	objID, err := primitive.ObjectIDFromHex(resourceID)
	if err != nil {
		h.logger.WithError(err).Error("Invalid resource ID format")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid resource ID"})
		return
	}

	// Get resource from database
	reqCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resource, err := h.resourceService.GetResourceByID(reqCtx, objID, userID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get resource")
		c.JSON(http.StatusNotFound, gin.H{"error": "Resource not found"})
		return
	}

	// Validate resource type - only pods support exec
	if resource.Type != models.ResourceTypePod {
		h.logger.WithFields(map[string]interface{}{
			"resourceType": resource.Type,
			"resourceName": resource.Name,
		}).Warn("Terminal access requested for non-pod resource")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only pods support terminal access"})
		return
	}

	// If no container specified, use the first container
	if container == "" && len(resource.Spec.Containers) > 0 {
		container = resource.Spec.Containers[0].Name
	}

	// Get Kubernetes cluster connection
	cluster, err := h.clusterService.GetClusterByName(reqCtx, userID, resource.ClusterName)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get cluster")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to cluster"})
		return
	}

	// Create streaming connection (no timeout)
	clientset, err := h.clusterService.CreateStreamingClusterConnection(cluster)
	if err != nil {
		h.logger.WithError(err).Error("Failed to create cluster connection")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to cluster"})
		return
	}

	// Get REST config for exec
	auth := h.clusterService.ClusterToAuthConfig(cluster)
	restConfig, err := auth.BuildRESTConfig()
	if err != nil {
		h.logger.WithError(err).Error("Failed to build REST config")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to configure cluster connection"})
		return
	}

	// Upgrade HTTP connection to WebSocket
	h.logger.WithFields(map[string]interface{}{
		"headers": c.Request.Header,
		"method":  c.Request.Method,
	}).Info("Attempting WebSocket upgrade")

	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.WithFields(map[string]interface{}{
			"error":   err.Error(),
			"headers": c.Request.Header,
		}).Error("Failed to upgrade to WebSocket")
		return
	}
	defer func() {
		if err := ws.Close(); err != nil {
			h.logger.WithError(err).Debug("Error closing WebSocket connection")
		}
	}()

	h.logger.Info("WebSocket upgraded successfully")

	// Send initial metadata
	metadata := TerminalMessage{
		Type: "metadata",
		Data: fmt.Sprintf(`{"pod":"%s","container":"%s","namespace":"%s","cluster":"%s"}`,
			resource.Name, container, resource.Namespace, resource.ClusterName),
	}
	if err := ws.WriteJSON(metadata); err != nil {
		h.logger.WithError(err).Error("Failed to send metadata")
		return
	}

	// Create terminal session
	session := &TerminalSession{
		ws:       ws,
		sizeChan: make(chan remotecommand.TerminalSize, 1),
		logger:   h.logger,
	}

	// Set initial terminal size (default)
	session.sizeChan <- remotecommand.TerminalSize{
		Width:  80,
		Height: 24,
	}

	// Create exec request
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(resource.Name).
		Namespace(resource.Namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   []string{shell},
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}, scheme.ParameterCodec)

	// Create SPDY executor
	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		h.logger.WithError(err).Error("Failed to create executor")
		if writeErr := ws.WriteJSON(TerminalMessage{Type: "error", Data: "Failed to create terminal session"}); writeErr != nil {
			h.logger.WithError(writeErr).Debug("Failed to send error message to WebSocket")
		}
		return
	}

	// Record access audit
	go func() {
		if err := h.resourceService.RecordResourceAccess(context.Background(), objID, userID, "exec", map[string]string{
			"container": container,
			"shell":     shell,
		}); err != nil {
			h.logger.WithError(err).Warn("Failed to record resource access")
		}
	}()

	// Create channels for input handling
	inputChan := make(chan []byte, 10)
	session.inputChan = inputChan

	// Handle all WebSocket messages in a single goroutine
	go func() {
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				h.logger.WithError(err).Debug("WebSocket read error")
				close(inputChan)
				return
			}

			var termMsg TerminalMessage
			if err := json.Unmarshal(msg, &termMsg); err != nil {
				h.logger.WithError(err).Warn("Failed to unmarshal terminal message")
				continue
			}

			h.logger.WithFields(map[string]interface{}{
				"type": termMsg.Type,
				"data_len": len(termMsg.Data),
			}).Debug("Received terminal message")

			switch termMsg.Type {
			case "input":
				// Send input to the channel for Read method to consume
				select {
				case inputChan <- []byte(termMsg.Data):
					h.logger.Debug("Input sent to channel")
				default:
					h.logger.Warn("Input channel full, dropping message")
				}
			case "resize":
				if termMsg.Rows > 0 && termMsg.Cols > 0 {
					select {
					case session.sizeChan <- remotecommand.TerminalSize{
						Width:  termMsg.Cols,
						Height: termMsg.Rows,
					}:
						h.logger.Debug("Resize event sent")
					default:
						h.logger.Debug("Resize channel full")
					}
				}
			}
		}
	}()

	// Stream the terminal session (blocks until session ends)
	h.logger.WithFields(map[string]interface{}{
		"pod":       resource.Name,
		"namespace": resource.Namespace,
		"container": container,
		"shell":     shell,
	}).Info("Starting terminal stream to pod")

	err = exec.StreamWithContext(context.Background(), remotecommand.StreamOptions{
		Stdin:             session,
		Stdout:            session,
		Stderr:            session,
		Tty:               true,
		TerminalSizeQueue: session,
	})

	if err != nil {
		h.logger.WithError(err).Error("Terminal session error")
		if writeErr := ws.WriteJSON(TerminalMessage{Type: "error", Data: "Terminal session ended unexpectedly"}); writeErr != nil {
			h.logger.WithError(writeErr).Debug("Failed to send error message to WebSocket")
		}
	} else {
		if writeErr := ws.WriteJSON(TerminalMessage{Type: "close", Data: "Terminal session completed"}); writeErr != nil {
			h.logger.WithError(writeErr).Debug("Failed to send close message to WebSocket")
		}
	}
}

// buildResourceSummary creates a summary object for a resource
func buildResourceSummary(r *models.Resource) map[string]interface{} {
	summary := make(map[string]interface{})

	switch r.Type {
	case models.ResourceTypeDeployment, models.ResourceTypeStatefulSet:
		if r.Spec.Replicas != nil && r.Spec.ReadyReplicas != nil {
			summary["replicas"] = fmt.Sprintf("%d/%d", *r.Spec.ReadyReplicas, *r.Spec.Replicas)
		}
		if r.Spec.AvailableReplicas != nil {
			summary["available"] = *r.Spec.AvailableReplicas
		}

	case models.ResourceTypePod:
		if len(r.Spec.Containers) > 0 {
			summary["containers"] = len(r.Spec.Containers)
			// Count restarts
			totalRestarts := int32(0)
			for _, c := range r.Spec.Containers {
				totalRestarts += c.RestartCount
			}
			summary["restarts"] = totalRestarts
		}
		summary["nodeName"] = r.Spec.NodeName
		summary["podIP"] = r.Spec.PodIP

	case models.ResourceTypeService:
		summary["type"] = r.Spec.ServiceType
		summary["clusterIP"] = r.Spec.ClusterIP
		if len(r.Spec.Ports) > 0 {
			summary["ports"] = len(r.Spec.Ports)
		}

	case models.ResourceTypeIngress:
		summary["class"] = r.Spec.IngressClass
		if len(r.Spec.IngressHosts) > 0 {
			summary["hosts"] = r.Spec.IngressHosts
		}
		summary["rules"] = r.Spec.IngressRules
		summary["paths"] = r.Spec.IngressPaths
		if r.Spec.LoadBalancerIP != "" {
			summary["loadBalancerIP"] = r.Spec.LoadBalancerIP
		}

	case models.ResourceTypeNode:
		summary["cpu"] = r.Spec.NodeCapacity.CPU
		summary["memory"] = r.Spec.NodeCapacity.Memory
	}

	// Calculate age
	age := time.Since(r.CreatedAt)
	if age.Hours() < 1 {
		summary["age"] = fmt.Sprintf("%dm", int(age.Minutes()))
	} else if age.Hours() < 24 {
		summary["age"] = fmt.Sprintf("%dh", int(age.Hours()))
	} else {
		summary["age"] = fmt.Sprintf("%dd", int(age.Hours()/24))
	}

	return summary
}

// StreamResourceStatus subscribes to resource status updates via unified SSE broadcaster
// This handler only subscribes to the broadcaster - watchers are started elsewhere when resources
// are created/updated via workflows
func (h *ResourcesHandler) StreamResourceStatus(c *gin.Context) {
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

	// Validate user owns resource
	resource, err := h.resourceService.GetResourceByID(c.Request.Context(), objID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Resource not found"})
		return
	}

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Subscribe to unified broadcaster
	broadcaster := services.GetSSEBroadcaster()
	streamKey := fmt.Sprintf("resource:%s", resourceID)
	eventChan := broadcaster.Subscribe(streamKey, 10)
	defer broadcaster.Unsubscribe(streamKey, eventChan)

	h.logger.WithField("resource_id", resourceID).Info("Client subscribed to resource status stream")

	// Send initial resource state
	initialData := map[string]interface{}{
		"id":        resource.ID.Hex(),
		"name":      resource.Name,
		"namespace": resource.Namespace,
		"type":      resource.Type,
		"status":    resource.Status,
		"spec":      resource.Spec,
	}
	initialJSON, err := json.Marshal(initialData)
	if err != nil {
		h.logger.WithError(err).Error("Failed to marshal resource metadata")
		c.SSEvent("error", "Failed to create stream metadata")
		c.Writer.Flush()
		return
	}
	c.SSEvent("metadata", string(initialJSON))
	c.Writer.Flush()

	// Relay events from broadcaster
	for {
		select {
		case <-c.Request.Context().Done():
			h.logger.WithField("resource_id", resourceID).Info("Client disconnected from resource status stream")
			return

		case event, ok := <-eventChan:
			if !ok {
				c.SSEvent("complete", "Stream closed")
				c.Writer.Flush()
				return
			}

			eventJSON, err := json.Marshal(event.Data)
			if err != nil {
				h.logger.WithError(err).Error("Failed to marshal event data")
				continue
			}
			c.SSEvent(event.EventType, string(eventJSON))
			c.Writer.Flush()

			// Check for write errors
			if c.Errors.Last() != nil {
				h.logger.WithError(c.Errors.Last()).Warn("Error writing SSE event")
				return
			}
		}
	}
}

