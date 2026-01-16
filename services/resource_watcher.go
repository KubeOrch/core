package services

import (
	"context"
	"fmt"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// ResourceWatcher watches any Kubernetes resource for status changes
type ResourceWatcher struct {
	workflowID    primitive.ObjectID
	nodeID        string
	resourceName  string
	namespace     string
	resourceType  string // "deployment", "service", "statefulset", etc.
	gvr           schema.GroupVersionResource
	dynamicClient dynamic.Interface
	stopChan      chan struct{}
	running       bool
	logger        *logrus.Entry
	lastStatus    map[string]interface{}
}

// ResourceWatcherConfig contains configuration for creating a resource watcher
type ResourceWatcherConfig struct {
	WorkflowID   primitive.ObjectID
	NodeID       string
	ResourceName string
	Namespace    string
	ResourceType string // "deployment", "service", "statefulset", "daemonset", "job", "cronjob", "ingress", etc.
	RestConfig   *rest.Config
}

// GVR mappings for common resource types
var resourceGVRMap = map[string]schema.GroupVersionResource{
	"deployment":  {Group: "apps", Version: "v1", Resource: "deployments"},
	"service":     {Group: "", Version: "v1", Resource: "services"},
	"statefulset": {Group: "apps", Version: "v1", Resource: "statefulsets"},
	"daemonset":   {Group: "apps", Version: "v1", Resource: "daemonsets"},
	"job":         {Group: "batch", Version: "v1", Resource: "jobs"},
	"cronjob":     {Group: "batch", Version: "v1", Resource: "cronjobs"},
	"ingress":     {Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
	"configmap":   {Group: "", Version: "v1", Resource: "configmaps"},
	"secret":      {Group: "", Version: "v1", Resource: "secrets"},
	"pvc":         {Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
	"pod":         {Group: "", Version: "v1", Resource: "pods"},
	"replicaset":  {Group: "apps", Version: "v1", Resource: "replicasets"},
	"hpa":         {Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"},
}

// NewResourceWatcher creates a new generic resource watcher
func NewResourceWatcher(config ResourceWatcherConfig) (*ResourceWatcher, error) {
	gvr, ok := resourceGVRMap[config.ResourceType]
	if !ok {
		return nil, fmt.Errorf("unsupported resource type: %s", config.ResourceType)
	}

	dynamicClient, err := dynamic.NewForConfig(config.RestConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	namespace := config.Namespace
	if namespace == "" {
		namespace = "default"
	}

	return &ResourceWatcher{
		workflowID:    config.WorkflowID,
		nodeID:        config.NodeID,
		resourceName:  config.ResourceName,
		namespace:     namespace,
		resourceType:  config.ResourceType,
		gvr:           gvr,
		dynamicClient: dynamicClient,
		stopChan:      make(chan struct{}),
		running:       false,
		logger: logrus.WithFields(logrus.Fields{
			"component":     "resource-watcher",
			"workflow_id":   config.WorkflowID.Hex(),
			"node_id":       config.NodeID,
			"resource_type": config.ResourceType,
			"resource_name": config.ResourceName,
			"namespace":     namespace,
		}),
	}, nil
}

// Start begins watching the resource
func (rw *ResourceWatcher) Start() error {
	if rw.running {
		return fmt.Errorf("watcher already running")
	}

	rw.running = true
	rw.logger.Info("Starting resource watcher")

	go rw.watchLoop()

	return nil
}

// Stop stops watching the resource
func (rw *ResourceWatcher) Stop() {
	if !rw.running {
		return
	}

	rw.logger.Info("Stopping resource watcher")
	close(rw.stopChan)
	rw.running = false
}

// IsRunning returns whether the watcher is currently running
func (rw *ResourceWatcher) IsRunning() bool {
	return rw.running
}

// GetResourceType returns the resource type being watched
func (rw *ResourceWatcher) GetResourceType() string {
	return rw.resourceType
}

// watchLoop is the main watch loop with reconnection logic
func (rw *ResourceWatcher) watchLoop() {
	retryDelay := 5 * time.Second
	maxRetryDelay := 60 * time.Second
	retryCount := 0

	for {
		select {
		case <-rw.stopChan:
			rw.logger.Info("Watch loop stopped")
			return
		default:
			err := rw.watch()
			if err != nil {
				if err == context.Canceled {
					rw.logger.Info("Watch canceled")
					return
				}

				retryCount++
				currentDelay := time.Duration(retryCount) * retryDelay
				if currentDelay > maxRetryDelay {
					currentDelay = maxRetryDelay
				}

				rw.logger.WithError(err).Warnf("Watch failed, retrying in %v (attempt %d)", currentDelay, retryCount)

				select {
				case <-time.After(currentDelay):
					continue
				case <-rw.stopChan:
					return
				}
			}

			retryCount = 0

			select {
			case <-time.After(1 * time.Second):
			case <-rw.stopChan:
				return
			}
		}
	}
}

// watch performs the actual K8s watch
func (rw *ResourceWatcher) watch() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-rw.stopChan
		cancel()
	}()

	// Get initial resource state
	resource, err := rw.dynamicClient.Resource(rw.gvr).Namespace(rw.namespace).Get(ctx, rw.resourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get initial resource state: %w", err)
	}

	// Extract and cache initial status
	rw.lastStatus = rw.extractStatus(resource)
	rw.logger.WithFields(logrus.Fields{
		"initial_status":   rw.lastStatus,
		"resource_version": resource.GetResourceVersion(),
	}).Info("Cached initial resource state")

	// If resource is already in a "ready" state, broadcast immediately
	if rw.isResourceReady(rw.lastStatus) {
		rw.logger.WithField("status", rw.lastStatus).Info("Resource already ready, broadcasting initial state")
		if err := rw.updateWorkflowStatus(rw.lastStatus); err != nil {
			rw.logger.WithError(err).Error("Failed to update workflow with initial status")
		}
		rw.broadcastStatusUpdate(rw.lastStatus)
	}

	// Create watcher starting from the resource version we just read
	watcher, err := rw.dynamicClient.Resource(rw.gvr).Namespace(rw.namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector:   fmt.Sprintf("metadata.name=%s", rw.resourceName),
		ResourceVersion: resource.GetResourceVersion(),
		Watch:           true,
	})
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer watcher.Stop()

	rw.logger.Info("Watch established successfully, waiting for events...")

	for event := range watcher.ResultChan() {
		rw.logger.WithField("event_type", event.Type).Debug("Received watch event")
		if event.Object == nil {
			continue
		}

		switch event.Type {
		case watch.Modified, watch.Added:
			unstrObj, ok := event.Object.(*unstructured.Unstructured)
			if !ok {
				continue
			}
			rw.handleResourceUpdate(unstrObj)

		case watch.Deleted:
			rw.logger.Info("Resource deleted, stopping watcher")
			rw.Stop()
			return nil

		case watch.Error:
			rw.logger.Error("Watch error event received")
			return fmt.Errorf("watch error")
		}
	}

	rw.logger.Info("Watch event channel closed, restarting watch...")
	return nil
}

// handleResourceUpdate processes a resource update event
func (rw *ResourceWatcher) handleResourceUpdate(resource *unstructured.Unstructured) {
	newStatus := rw.extractStatus(resource)

	if rw.hasStatusChanged(rw.lastStatus, newStatus) {
		rw.logger.WithFields(logrus.Fields{
			"old_status": rw.lastStatus,
			"new_status": newStatus,
		}).Info("Resource status changed")

		if err := rw.updateWorkflowStatus(newStatus); err != nil {
			rw.logger.WithError(err).Error("Failed to update workflow status")
		}

		rw.broadcastStatusUpdate(newStatus)
		rw.lastStatus = newStatus
	}
}

// extractStatus extracts status from any resource type
func (rw *ResourceWatcher) extractStatus(resource *unstructured.Unstructured) map[string]interface{} {
	switch rw.resourceType {
	case "deployment":
		return rw.extractDeploymentStatus(resource)
	case "service":
		return rw.extractServiceStatus(resource)
	case "statefulset":
		return rw.extractStatefulSetStatus(resource)
	case "daemonset":
		return rw.extractDaemonSetStatus(resource)
	case "job":
		return rw.extractJobStatus(resource)
	case "pod":
		return rw.extractPodStatus(resource)
	default:
		// Generic status extraction for unknown types
		return rw.extractGenericStatus(resource)
	}
}

// extractDeploymentStatus extracts deployment-specific status
func (rw *ResourceWatcher) extractDeploymentStatus(resource *unstructured.Unstructured) map[string]interface{} {
	replicas, _, _ := unstructured.NestedInt64(resource.Object, "status", "replicas")
	readyReplicas, _, _ := unstructured.NestedInt64(resource.Object, "status", "readyReplicas")
	desiredReplicas, _, _ := unstructured.NestedInt64(resource.Object, "spec", "replicas")

	if desiredReplicas == 0 {
		desiredReplicas = 1
	}

	state := "healthy"
	message := ""

	// Check if deployment is just starting (no replicas created yet)
	if replicas == 0 && readyReplicas == 0 && desiredReplicas > 0 {
		state = "partial"
		message = "Starting deployment..."
	} else if readyReplicas == 0 && desiredReplicas > 0 {
		state = "error"
		message = "No pods are ready"
	} else if readyReplicas < desiredReplicas {
		state = "partial"
		message = fmt.Sprintf("%d/%d pods ready", readyReplicas, desiredReplicas)
	} else if readyReplicas > 0 {
		message = fmt.Sprintf("All %d pods ready", readyReplicas)
	}

	return map[string]interface{}{
		"replicas":      replicas,
		"readyReplicas": readyReplicas,
		"state":         state,
		"message":       message,
	}
}

// extractServiceStatus extracts service-specific status
func (rw *ResourceWatcher) extractServiceStatus(resource *unstructured.Unstructured) map[string]interface{} {
	clusterIP, _, _ := unstructured.NestedString(resource.Object, "spec", "clusterIP")
	serviceType, _, _ := unstructured.NestedString(resource.Object, "spec", "type")

	// Get node port from first port
	var nodePort int64
	ports, _, _ := unstructured.NestedSlice(resource.Object, "spec", "ports")
	if len(ports) > 0 {
		if port, ok := ports[0].(map[string]interface{}); ok {
			if np, ok := port["nodePort"].(int64); ok {
				nodePort = np
			}
		}
	}

	externalIP := ""
	state := "healthy"
	message := ""

	if serviceType == "LoadBalancer" {
		ingress, _, _ := unstructured.NestedSlice(resource.Object, "status", "loadBalancer", "ingress")
		if len(ingress) > 0 {
			if ing, ok := ingress[0].(map[string]interface{}); ok {
				if ip, ok := ing["ip"].(string); ok && ip != "" {
					externalIP = ip
				} else if hostname, ok := ing["hostname"].(string); ok && hostname != "" {
					externalIP = hostname
				}
			}
		}

		if externalIP == "" {
			state = "partial"
			message = "Waiting for LoadBalancer IP assignment"
		}
	}

	return map[string]interface{}{
		"clusterIP":  clusterIP,
		"externalIP": externalIP,
		"nodePort":   nodePort,
		"state":      state,
		"message":    message,
	}
}

// extractStatefulSetStatus extracts statefulset-specific status
func (rw *ResourceWatcher) extractStatefulSetStatus(resource *unstructured.Unstructured) map[string]interface{} {
	replicas, _, _ := unstructured.NestedInt64(resource.Object, "status", "replicas")
	readyReplicas, _, _ := unstructured.NestedInt64(resource.Object, "status", "readyReplicas")
	desiredReplicas, _, _ := unstructured.NestedInt64(resource.Object, "spec", "replicas")

	state := "healthy"
	message := ""

	if readyReplicas == 0 && desiredReplicas > 0 {
		state = "error"
		message = "No pods are ready"
	} else if readyReplicas < desiredReplicas {
		state = "partial"
		message = fmt.Sprintf("%d/%d pods ready", readyReplicas, desiredReplicas)
	} else if readyReplicas > 0 {
		message = fmt.Sprintf("All %d pods ready", readyReplicas)
	}

	return map[string]interface{}{
		"replicas":      replicas,
		"readyReplicas": readyReplicas,
		"state":         state,
		"message":       message,
	}
}

// extractDaemonSetStatus extracts daemonset-specific status
func (rw *ResourceWatcher) extractDaemonSetStatus(resource *unstructured.Unstructured) map[string]interface{} {
	desiredNumberScheduled, _, _ := unstructured.NestedInt64(resource.Object, "status", "desiredNumberScheduled")
	numberReady, _, _ := unstructured.NestedInt64(resource.Object, "status", "numberReady")

	state := "healthy"
	message := ""

	if numberReady == 0 && desiredNumberScheduled > 0 {
		state = "error"
		message = "No pods are ready"
	} else if numberReady < desiredNumberScheduled {
		state = "partial"
		message = fmt.Sprintf("%d/%d pods ready", numberReady, desiredNumberScheduled)
	} else if numberReady > 0 {
		message = fmt.Sprintf("All %d pods ready", numberReady)
	}

	return map[string]interface{}{
		"desiredNumberScheduled": desiredNumberScheduled,
		"numberReady":            numberReady,
		"state":                  state,
		"message":                message,
	}
}

// extractJobStatus extracts job-specific status
func (rw *ResourceWatcher) extractJobStatus(resource *unstructured.Unstructured) map[string]interface{} {
	active, _, _ := unstructured.NestedInt64(resource.Object, "status", "active")
	succeeded, _, _ := unstructured.NestedInt64(resource.Object, "status", "succeeded")
	failed, _, _ := unstructured.NestedInt64(resource.Object, "status", "failed")

	state := "running"
	message := ""

	if succeeded > 0 {
		state = "healthy"
		message = fmt.Sprintf("Completed successfully (%d succeeded)", succeeded)
	} else if failed > 0 {
		state = "error"
		message = fmt.Sprintf("Failed (%d failed)", failed)
	} else if active > 0 {
		state = "partial"
		message = fmt.Sprintf("Running (%d active)", active)
	}

	return map[string]interface{}{
		"active":    active,
		"succeeded": succeeded,
		"failed":    failed,
		"state":     state,
		"message":   message,
	}
}

// extractPodStatus extracts pod-specific status
func (rw *ResourceWatcher) extractPodStatus(resource *unstructured.Unstructured) map[string]interface{} {
	phase, _, _ := unstructured.NestedString(resource.Object, "status", "phase")

	state := "partial"
	message := phase

	switch phase {
	case "Running":
		state = "healthy"
	case "Succeeded":
		state = "healthy"
	case "Failed":
		state = "error"
	case "Pending":
		state = "partial"
	}

	return map[string]interface{}{
		"phase":   phase,
		"state":   state,
		"message": message,
	}
}

// extractGenericStatus extracts generic status for unknown resource types
func (rw *ResourceWatcher) extractGenericStatus(resource *unstructured.Unstructured) map[string]interface{} {
	status, found, _ := unstructured.NestedMap(resource.Object, "status")
	if !found {
		return map[string]interface{}{
			"state":   "unknown",
			"message": "No status available",
		}
	}

	// Try to determine state from common conditions
	state := "healthy"
	message := ""

	conditions, found, _ := unstructured.NestedSlice(status, "conditions")
	if found && len(conditions) > 0 {
		for _, c := range conditions {
			if cond, ok := c.(map[string]interface{}); ok {
				condType, _ := cond["type"].(string)
				condStatus, _ := cond["status"].(string)
				if condType == "Ready" || condType == "Available" {
					if condStatus != "True" {
						state = "partial"
						if msg, ok := cond["message"].(string); ok {
							message = msg
						}
					}
				}
			}
		}
	}

	result := map[string]interface{}{
		"state":   state,
		"message": message,
	}

	// Include raw status for debugging
	for k, v := range status {
		if k != "conditions" {
			result[k] = v
		}
	}

	return result
}

// isResourceReady checks if resource is in a ready state based on type
func (rw *ResourceWatcher) isResourceReady(status map[string]interface{}) bool {
	state, ok := status["state"].(string)
	if !ok {
		return false
	}
	return state == "healthy"
}

// hasStatusChanged compares old and new status
func (rw *ResourceWatcher) hasStatusChanged(old, new map[string]interface{}) bool {
	if old == nil {
		return true
	}

	// Compare key fields based on resource type
	switch rw.resourceType {
	case "deployment", "statefulset":
		return old["readyReplicas"] != new["readyReplicas"] ||
			old["state"] != new["state"]
	case "service":
		return old["externalIP"] != new["externalIP"] ||
			old["state"] != new["state"] ||
			old["clusterIP"] != new["clusterIP"]
	case "job":
		return old["active"] != new["active"] ||
			old["succeeded"] != new["succeeded"] ||
			old["failed"] != new["failed"]
	case "pod":
		return old["phase"] != new["phase"]
	default:
		return old["state"] != new["state"]
	}
}

// updateWorkflowStatus updates the workflow node status in MongoDB
func (rw *ResourceWatcher) updateWorkflowStatus(status map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{
		"_id":      rw.workflowID,
		"nodes.id": rw.nodeID,
	}

	update := bson.M{
		"$set": bson.M{
			"nodes.$._status": status,
			"updated_at":      time.Now(),
		},
	}

	result, err := database.WorkflowColl.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update workflow: %w", err)
	}

	if result.ModifiedCount == 0 {
		rw.logger.Warn("No workflow updated - node may have been deleted")
	}

	return nil
}

// broadcastStatusUpdate sends an SSE event to subscribers
func (rw *ResourceWatcher) broadcastStatusUpdate(status map[string]interface{}) {
	broadcaster := GetSSEBroadcaster()

	broadcaster.Publish(StreamEvent{
		Type:      "workflow",
		StreamKey: fmt.Sprintf("workflow:%s", rw.workflowID.Hex()),
		EventType: "node_update",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"node_id": rw.nodeID,
			"type":    rw.resourceType,
			"status":  status,
		},
	})

	rw.logger.Debug("Broadcasted status update to SSE subscribers")
}
