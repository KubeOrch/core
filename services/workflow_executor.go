package services

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/pkg/applier"
	"github.com/KubeOrch/core/pkg/template"
	"github.com/KubeOrch/core/pkg/validator"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// WorkflowExecutor handles workflow execution
type WorkflowExecutor struct {
	clusterService   *KubernetesClusterService
	templateEngine   *template.Engine
	validator        *validator.ResourceValidator
	logger           *logrus.Logger
}

// NewWorkflowExecutor creates a new workflow executor
func NewWorkflowExecutor() *WorkflowExecutor {
	// Get templates directory path
	templatesDir := filepath.Join(".", "templates")

	return &WorkflowExecutor{
		clusterService:   NewKubernetesClusterService(),
		templateEngine:   template.NewEngine(templatesDir),
		validator:        validator.NewResourceValidator(),
		logger:           logrus.New(),
	}
}

// ExecuteWorkflow executes a workflow with optional runtime data (e.g., secret values)
func (e *WorkflowExecutor) ExecuteWorkflow(ctx context.Context, workflowID primitive.ObjectID, userID primitive.ObjectID, runtimeData map[string]interface{}) (*models.WorkflowRun, error) {
	// Get workflow
	workflow, err := GetWorkflowByID(workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow: %w", err)
	}

	// Ensure runtimeData is not nil
	if runtimeData == nil {
		runtimeData = make(map[string]interface{})
	}

	// Create workflow run record
	workflowRun := &models.WorkflowRun{
		WorkflowID:  workflowID,
		Version:     workflow.CurrentVersion,
		Status:      models.WorkflowRunStatusRunning,
		StartedAt:   time.Now(),
		NodeStates:  make(map[string]interface{}),
		Output:      make(map[string]interface{}),
		Logs:        []string{},
		TriggeredBy: "manual",
		TriggerData: map[string]interface{}{
			"user_id": userID,
		},
	}

	// Save initial workflow run
	if err := e.saveWorkflowRun(workflowRun); err != nil {
		return nil, fmt.Errorf("failed to save workflow run: %w", err)
	}

	cluster, err := e.getClusterForWorkflow(ctx, workflow.ClusterID, userID)
	if err != nil {
		e.updateWorkflowRunStatus(workflowRun, models.WorkflowRunStatusFailed, err.Error())
		e.updateWorkflowStats(workflowID, false)
		return workflowRun, fmt.Errorf("failed to get cluster: %w", err)
	}

	auth := e.clusterService.ClusterToAuthConfig(cluster)
	config, err := auth.BuildRESTConfig()
	if err != nil {
		e.updateWorkflowRunStatus(workflowRun, models.WorkflowRunStatusFailed, err.Error())
		e.updateWorkflowStats(workflowID, false)
		return workflowRun, fmt.Errorf("failed to build REST config: %w", err)
	}

	manifestApplier, err := applier.NewManifestApplier(config, "default")
	if err != nil {
		e.updateWorkflowRunStatus(workflowRun, models.WorkflowRunStatusFailed, err.Error())
		e.updateWorkflowStats(workflowID, false)
		return workflowRun, fmt.Errorf("failed to create manifest applier: %w", err)
	}

	// Build node map and connection graph
	nodeMap := e.buildNodeMap(workflow.Nodes)
	connectionGraph := e.buildConnectionGraph(workflow.Edges)

	// Get execution order using topological sort
	executionOrder, err := e.getExecutionOrder(workflow.Nodes, workflow.Edges)
	if err != nil {
		e.updateWorkflowRunStatus(workflowRun, models.WorkflowRunStatusFailed, err.Error())
		e.updateWorkflowStats(workflowID, false)
		return workflowRun, fmt.Errorf("invalid workflow structure: %w", err)
	}

	// Track executed node data for passing between connected nodes
	executedNodeData := make(map[string]map[string]interface{})

	workflowRun.Logs = append(workflowRun.Logs, fmt.Sprintf("[%s] Execution order: %v",
		time.Now().Format("15:04:05"), executionOrder))

	for _, nodeID := range executionOrder {
		node, exists := nodeMap[nodeID]
		if !exists {
			continue
		}

		// Get connected source nodes data
		connectedData := e.getConnectedSourceData(nodeID, connectionGraph, executedNodeData)

		switch node.Type {
		case "deployment":
			if err := e.executeDeploymentNode(ctx, manifestApplier, node, workflowRun); err != nil {
				e.updateWorkflowRunStatus(workflowRun, models.WorkflowRunStatusFailed, err.Error())
				e.updateWorkflowStats(workflowID, false)
				return workflowRun, fmt.Errorf("failed to execute node %s: %w", node.ID, err)
			}
			// Store deployment data for connected nodes
			executedNodeData[node.ID] = node.Data
		case "service":
			if err := e.executeServiceNodeWithConnections(ctx, manifestApplier, node, workflowRun, connectedData); err != nil {
				e.updateWorkflowRunStatus(workflowRun, models.WorkflowRunStatusFailed, err.Error())
				e.updateWorkflowStats(workflowID, false)
				return workflowRun, fmt.Errorf("failed to execute node %s: %w", node.ID, err)
			}
			// Store service data for any downstream nodes
			executedNodeData[node.ID] = node.Data
		case "ingress":
			if err := e.executeIngressNodeWithConnections(ctx, manifestApplier, node, workflowRun, connectedData); err != nil {
				e.updateWorkflowRunStatus(workflowRun, models.WorkflowRunStatusFailed, err.Error())
				e.updateWorkflowStats(workflowID, false)
				return workflowRun, fmt.Errorf("failed to execute node %s: %w", node.ID, err)
			}
			// Store ingress data for any downstream nodes
			executedNodeData[node.ID] = node.Data
		case "configmap":
			if err := e.executeConfigMapNode(ctx, manifestApplier, node, workflowRun); err != nil {
				e.updateWorkflowRunStatus(workflowRun, models.WorkflowRunStatusFailed, err.Error())
				e.updateWorkflowStats(workflowID, false)
				return workflowRun, fmt.Errorf("failed to execute node %s: %w", node.ID, err)
			}
			// Store configmap data for connected deployment nodes
			executedNodeData[node.ID] = node.Data
		case "secret":
			if err := e.executeSecretNode(ctx, manifestApplier, node, workflowRun, runtimeData); err != nil {
				e.updateWorkflowRunStatus(workflowRun, models.WorkflowRunStatusFailed, err.Error())
				e.updateWorkflowStats(workflowID, false)
				return workflowRun, fmt.Errorf("failed to execute node %s: %w", node.ID, err)
			}
			// Store secret data for connected deployment nodes
			executedNodeData[node.ID] = node.Data
		}
	}
	completedAt := time.Now()
	workflowRun.CompletedAt = &completedAt
	workflowRun.Duration = int64(completedAt.Sub(workflowRun.StartedAt).Milliseconds())
	e.updateWorkflowRunStatus(workflowRun, models.WorkflowRunStatusCompleted, "Workflow executed successfully")

	// Update workflow statistics
	e.updateWorkflowStats(workflowID, true)

	return workflowRun, nil
}

// buildNodeMap creates a map of node ID to node for quick lookup
func (e *WorkflowExecutor) buildNodeMap(nodes []models.WorkflowNode) map[string]*models.WorkflowNode {
	nodeMap := make(map[string]*models.WorkflowNode)
	for i := range nodes {
		nodeMap[nodes[i].ID] = &nodes[i]
	}
	return nodeMap
}

// buildConnectionGraph builds a map of target node ID to its source node IDs
func (e *WorkflowExecutor) buildConnectionGraph(edges []models.WorkflowEdge) map[string][]string {
	// Map: targetNodeID -> []sourceNodeIDs
	graph := make(map[string][]string)
	for _, edge := range edges {
		graph[edge.Target] = append(graph[edge.Target], edge.Source)
	}
	return graph
}

// ErrWorkflowCycleDetected is returned when a workflow contains a cycle
var ErrWorkflowCycleDetected = fmt.Errorf("workflow contains a cycle - workflows must be directed acyclic graphs (DAGs)")

// getExecutionOrder returns nodes in topological order based on edges.
// Returns an error if the workflow contains a cycle.
func (e *WorkflowExecutor) getExecutionOrder(nodes []models.WorkflowNode, edges []models.WorkflowEdge) ([]string, error) {
	// Build adjacency list and in-degree count
	inDegree := make(map[string]int)
	adjacencyList := make(map[string][]string)

	// Initialize all nodes with 0 in-degree
	for _, node := range nodes {
		inDegree[node.ID] = 0
		adjacencyList[node.ID] = []string{}
	}

	// Process edges
	for _, edge := range edges {
		adjacencyList[edge.Source] = append(adjacencyList[edge.Source], edge.Target)
		inDegree[edge.Target]++
	}

	// Queue for nodes with no incoming edges
	var queue []string
	for _, node := range nodes {
		if inDegree[node.ID] == 0 {
			queue = append(queue, node.ID)
		}
	}

	var result []string
	for len(queue) > 0 {
		// Pop from queue
		nodeID := queue[0]
		queue = queue[1:]
		result = append(result, nodeID)

		// Process all outgoing edges
		for _, targetID := range adjacencyList[nodeID] {
			inDegree[targetID]--
			if inDegree[targetID] == 0 {
				queue = append(queue, targetID)
			}
		}
	}

	// If result doesn't contain all nodes, there's a cycle
	if len(result) < len(nodes) {
		return nil, ErrWorkflowCycleDetected
	}

	return result, nil
}

// getConnectedSourceData retrieves data from all source nodes connected to this node
func (e *WorkflowExecutor) getConnectedSourceData(nodeID string, connectionGraph map[string][]string, executedNodeData map[string]map[string]interface{}) []map[string]interface{} {
	var connectedData []map[string]interface{}

	sourceIDs := connectionGraph[nodeID]
	for _, sourceID := range sourceIDs {
		if data, exists := executedNodeData[sourceID]; exists {
			connectedData = append(connectedData, data)
		}
	}

	return connectedData
}

// executeDeploymentNode executes a deployment node
func (e *WorkflowExecutor) executeDeploymentNode(ctx context.Context, manifestApplier *applier.ManifestApplier, node *models.WorkflowNode, run *models.WorkflowRun) error {
	e.logger.WithFields(logrus.Fields{
		"node_id":   node.ID,
		"node_type": node.Type,
	}).Info("Executing deployment node")

	// Add log entry
	run.Logs = append(run.Logs, fmt.Sprintf("[%s] Executing deployment node: %s",
		time.Now().Format("15:04:05"), node.ID))

	// Extract deployment data from node
	// The UI sends deployment data directly in node.Data
	// node.Data is already map[string]interface{}, no need to type assert
	deploymentData := node.Data
	if deploymentData == nil {
		return fmt.Errorf("invalid deployment data in node")
	}

	// Prepare template values
	templateValues := e.prepareTemplateValues(node, deploymentData)

	// Get template ID (default to core/deployment)
	templateID := "core/deployment"
	if tid, ok := deploymentData["templateId"].(string); ok {
		templateID = tid
	}

	// Validate parameters based on resource type
	validationResult, err := e.validator.ValidateResourceParams(templateID, templateValues)
	if err != nil {
		return fmt.Errorf("validation error: %w", err)
	}
	if !validationResult.Valid {
		return fmt.Errorf("validation failed: %v", validationResult.Errors)
	}

	// Render template to YAML
	renderedYAML, err := e.templateEngine.RenderTemplate(templateID, templateValues)
	if err != nil {
		return fmt.Errorf("failed to render template: %w", err)
	}

	// Apply the rendered YAML directly to Kubernetes
	result, err := manifestApplier.ApplyYAML(ctx, renderedYAML)
	if err != nil {
		run.NodeStates[node.ID] = map[string]interface{}{
			"status": "failed",
			"error":  err.Error(),
		}
		// Broadcast error to SSE subscribers so frontend shows the error
		e.broadcastNodeError(run.WorkflowID, node.ID, "deployment", err.Error())
		return fmt.Errorf("failed to apply manifest: %w", err)
	}

	// Update node state
	run.NodeStates[node.ID] = map[string]interface{}{
		"status":    "completed",
		"result":    result,
		"timestamp": time.Now().Unix(),
	}

	// Log the operation performed
	if len(result.AppliedResources) > 0 {
		resource := result.AppliedResources[0]
		run.Logs = append(run.Logs, fmt.Sprintf("[%s] Deployment %s/%s: %s",
			time.Now().Format("15:04:05"), resource.Namespace, resource.Name, resource.Operation))

		// Fetch and save deployment status to workflow node
		deploymentName := resource.Name
		namespace := resource.Namespace
		if namespace == "" {
			namespace = "default"
		}

		deploymentStatus, err := manifestApplier.GetDeploymentStatus(ctx, deploymentName, namespace)
		if err != nil {
			e.logger.WithError(err).Warn("Failed to get deployment status")
		} else {
			// Update workflow node data with status
			if err := e.updateDeploymentNodeStatus(run.WorkflowID, node.ID, deploymentStatus); err != nil {
				e.logger.WithError(err).Warn("Failed to update deployment node status in workflow")
			} else {
				run.Logs = append(run.Logs, fmt.Sprintf("[%s] Deployment status: %s, Replicas: %d/%d",
					time.Now().Format("15:04:05"), deploymentStatus.State, deploymentStatus.ReadyReplicas, deploymentStatus.Replicas))
			}
		}

		// Start watching the deployment for real-time status updates (pod readiness)
		watcherManager := GetResourceWatcherManager()
		restConfig := manifestApplier.GetRestConfig()

		err = watcherManager.StartWatcher(run.WorkflowID, node.ID, deploymentName, namespace, "deployment", restConfig)
		if err != nil {
			e.logger.WithError(err).Warn("Failed to start deployment watcher (falling back to periodic polling)")
		} else {
			e.logger.WithFields(logrus.Fields{
				"deployment_name": deploymentName,
				"namespace":       namespace,
				"node_id":         node.ID,
			}).Info("Started watching deployment for pod readiness")
			run.Logs = append(run.Logs, fmt.Sprintf("[%s] Watching deployment for pod readiness",
				time.Now().Format("15:04:05")))
		}
	}

	// Add to output
	run.Output[node.ID] = result

	// Add log entries for each applied resource
	for _, resource := range result.AppliedResources {
		run.Logs = append(run.Logs, fmt.Sprintf("[%s] %s %s/%s: %s",
			time.Now().Format("15:04:05"), resource.Kind, resource.Namespace, resource.Name, resource.Operation))
	}

	// Save updated workflow run
	if err := e.saveWorkflowRun(run); err != nil {
		e.logger.WithError(err).Error("Failed to save updated workflow run")
	}

	return nil
}

// executeServiceNodeWithConnections executes a service node with data from connected nodes
func (e *WorkflowExecutor) executeServiceNodeWithConnections(ctx context.Context, manifestApplier *applier.ManifestApplier, node *models.WorkflowNode, run *models.WorkflowRun, connectedData []map[string]interface{}) error {
	e.logger.WithFields(logrus.Fields{
		"node_id":         node.ID,
		"node_type":       node.Type,
		"connected_nodes": len(connectedData),
	}).Info("Executing service node with connections")

	// Add log entry
	run.Logs = append(run.Logs, fmt.Sprintf("[%s] Executing service node: %s (connected to %d source nodes)",
		time.Now().Format("15:04:05"), node.ID, len(connectedData)))

	// Extract service data from node
	serviceData := node.Data
	if serviceData == nil {
		serviceData = make(map[string]interface{})
	}

	// Apply connected deployment data to service
	// If connected to a deployment, auto-populate targetApp and port if not set
	for _, sourceData := range connectedData {
		// If targetApp is not set or empty, use connected deployment's name
		existingTargetApp, _ := serviceData["targetApp"].(string)
		if existingTargetApp == "" {
			if deploymentName, ok := sourceData["name"].(string); ok {
				serviceData["targetApp"] = deploymentName
				run.Logs = append(run.Logs, fmt.Sprintf("[%s] Auto-linked service to deployment: %s",
					time.Now().Format("15:04:05"), deploymentName))
			}
		}

		// If targetPort is not set, use connected deployment's port
		if _, hasTargetPort := serviceData["targetPort"]; !hasTargetPort {
			if port, ok := sourceData["port"]; ok {
				serviceData["targetPort"] = port
				run.Logs = append(run.Logs, fmt.Sprintf("[%s] Auto-set targetPort from deployment: %v",
					time.Now().Format("15:04:05"), port))
			}
		}

		// Inherit namespace from deployment if not set
		if _, hasNamespace := serviceData["namespace"]; !hasNamespace {
			if namespace, ok := sourceData["namespace"].(string); ok {
				serviceData["namespace"] = namespace
			}
		}
	}

	// Prepare template values for service
	templateValues := e.prepareServiceTemplateValues(node, serviceData)

	// Get template ID (default to core/service)
	templateID := "core/service"
	if tid, ok := serviceData["templateId"].(string); ok {
		templateID = tid
	}

	// Validate parameters based on resource type
	validationResult, err := e.validator.ValidateResourceParams(templateID, templateValues)
	if err != nil {
		return fmt.Errorf("validation error: %w", err)
	}
	if !validationResult.Valid {
		return fmt.Errorf("validation failed: %v", validationResult.Errors)
	}

	// Render template to YAML
	renderedYAML, err := e.templateEngine.RenderTemplate(templateID, templateValues)
	if err != nil {
		return fmt.Errorf("failed to render template: %w", err)
	}

	// Apply the rendered YAML directly to Kubernetes
	result, err := manifestApplier.ApplyYAML(ctx, renderedYAML)
	if err != nil {
		run.NodeStates[node.ID] = map[string]interface{}{
			"status": "failed",
			"error":  err.Error(),
		}
		// Broadcast error to SSE subscribers so frontend shows the error
		e.broadcastNodeError(run.WorkflowID, node.ID, "service", err.Error())
		return fmt.Errorf("failed to apply manifest: %w", err)
	}

	// Update node state
	run.NodeStates[node.ID] = map[string]interface{}{
		"status":        "completed",
		"result":        result,
		"timestamp":     time.Now().Unix(),
		"connectedFrom": len(connectedData),
	}

	// Log the operation performed
	if len(result.AppliedResources) > 0 {
		resource := result.AppliedResources[0]
		run.Logs = append(run.Logs, fmt.Sprintf("[%s] Service %s/%s: %s",
			time.Now().Format("15:04:05"), resource.Namespace, resource.Name, resource.Operation))

		// Fetch and save service status to workflow node
		serviceName := resource.Name
		namespace := resource.Namespace
		if namespace == "" {
			namespace = "default"
		}

		serviceStatus, err := manifestApplier.GetServiceStatus(ctx, serviceName, namespace)
		if err != nil {
			e.logger.WithError(err).Warn("Failed to get service status")
		} else {
			// Update workflow node data with status
			if err := e.updateNodeStatus(run.WorkflowID, node.ID, serviceStatus); err != nil {
				e.logger.WithError(err).Warn("Failed to update node status in workflow")
			} else {
				run.Logs = append(run.Logs, fmt.Sprintf("[%s] Service status: %s, ClusterIP: %s",
					time.Now().Format("15:04:05"), serviceStatus.State, serviceStatus.ClusterIP))
			}

			// Start watching the service for real-time status updates (LoadBalancer IP assignment)
			// Only watch LoadBalancer services as they need time to get external IP
			serviceType, _ := serviceData["serviceType"].(string)
			if serviceType == "LoadBalancer" {
				watcherManager := GetResourceWatcherManager()
				restConfig := manifestApplier.GetRestConfig()

				err := watcherManager.StartWatcher(run.WorkflowID, node.ID, serviceName, namespace, "service", restConfig)
				if err != nil {
					e.logger.WithError(err).Warn("Failed to start service watcher (falling back to periodic polling)")
				} else {
					e.logger.WithFields(logrus.Fields{
						"service_name": serviceName,
						"namespace":    namespace,
						"node_id":      node.ID,
					}).Info("Started watching service for LoadBalancer IP assignment")
					run.Logs = append(run.Logs, fmt.Sprintf("[%s] Watching service for LoadBalancer IP assignment",
						time.Now().Format("15:04:05")))
				}
			}
		}
	}

	// Add to output
	run.Output[node.ID] = result

	// Add log entries for each applied resource
	for _, resource := range result.AppliedResources {
		run.Logs = append(run.Logs, fmt.Sprintf("[%s] %s %s/%s: %s",
			time.Now().Format("15:04:05"), resource.Kind, resource.Namespace, resource.Name, resource.Operation))
	}

	// Save updated workflow run
	if err := e.saveWorkflowRun(run); err != nil {
		e.logger.WithError(err).Error("Failed to save updated workflow run")
	}

	return nil
}

// executeIngressNodeWithConnections executes an ingress node with data from connected nodes
func (e *WorkflowExecutor) executeIngressNodeWithConnections(ctx context.Context, manifestApplier *applier.ManifestApplier, node *models.WorkflowNode, run *models.WorkflowRun, connectedData []map[string]interface{}) error {
	e.logger.WithFields(logrus.Fields{
		"node_id":         node.ID,
		"node_type":       node.Type,
		"connected_nodes": len(connectedData),
	}).Info("Executing ingress node with connections")

	// Add log entry
	run.Logs = append(run.Logs, fmt.Sprintf("[%s] Executing ingress node: %s (connected to %d source nodes)",
		time.Now().Format("15:04:05"), node.ID, len(connectedData)))

	// Extract ingress data from node
	ingressData := node.Data
	if ingressData == nil {
		ingressData = make(map[string]interface{})
	}

	// Apply connected service data to ingress
	// If connected to a service, auto-populate serviceName and servicePort if not set
	for _, sourceData := range connectedData {
		// If serviceName is not set or empty, use connected service's name
		existingServiceName, _ := ingressData["serviceName"].(string)
		if existingServiceName == "" {
			if serviceName, ok := sourceData["name"].(string); ok {
				ingressData["serviceName"] = serviceName
				run.Logs = append(run.Logs, fmt.Sprintf("[%s] Auto-linked ingress to service: %s",
					time.Now().Format("15:04:05"), serviceName))
			}
		}

		// If servicePort is not set, use connected service's port
		if _, hasServicePort := ingressData["servicePort"]; !hasServicePort {
			if port, ok := sourceData["port"]; ok {
				ingressData["servicePort"] = port
				run.Logs = append(run.Logs, fmt.Sprintf("[%s] Auto-set servicePort from service: %v",
					time.Now().Format("15:04:05"), port))
			}
		}

		// Inherit namespace from service if not set
		if _, hasNamespace := ingressData["namespace"]; !hasNamespace {
			if namespace, ok := sourceData["namespace"].(string); ok {
				ingressData["namespace"] = namespace
			}
		}
	}

	// Prepare template values for ingress
	templateValues := e.prepareIngressTemplateValues(node, ingressData)

	// Debug: Log template values
	e.logger.WithFields(logrus.Fields{
		"node_id":         node.ID,
		"template_values": templateValues,
		"ingress_data":    ingressData,
	}).Info("Prepared ingress template values")

	// Get template ID (default to core/ingress)
	templateID := "core/ingress"
	if tid, ok := ingressData["templateId"].(string); ok {
		templateID = tid
	}

	// Validate parameters based on resource type
	validationResult, err := e.validator.ValidateResourceParams(templateID, templateValues)
	if err != nil {
		return fmt.Errorf("validation error: %w", err)
	}
	if !validationResult.Valid {
		return fmt.Errorf("validation failed: %v", validationResult.Errors)
	}

	// Render template to YAML
	renderedYAML, err := e.templateEngine.RenderTemplate(templateID, templateValues)
	if err != nil {
		return fmt.Errorf("failed to render template: %w", err)
	}

	// Debug: Log rendered YAML
	e.logger.WithFields(logrus.Fields{
		"node_id":       node.ID,
		"rendered_yaml": renderedYAML,
	}).Info("Rendered ingress YAML")

	// Apply the rendered YAML directly to Kubernetes
	result, err := manifestApplier.ApplyYAML(ctx, renderedYAML)
	if err != nil {
		run.NodeStates[node.ID] = map[string]interface{}{
			"status": "failed",
			"error":  err.Error(),
		}
		e.logger.WithError(err).WithField("rendered_yaml", renderedYAML).Error("Failed to apply ingress manifest")
		// Broadcast error to SSE subscribers so frontend shows the error
		e.broadcastNodeError(run.WorkflowID, node.ID, "ingress", err.Error())
		return fmt.Errorf("failed to apply manifest: %w", err)
	}

	// Update node state
	run.NodeStates[node.ID] = map[string]interface{}{
		"status":        "completed",
		"result":        result,
		"timestamp":     time.Now().Unix(),
		"connectedFrom": len(connectedData),
	}

	// Log the operation performed
	if len(result.AppliedResources) > 0 {
		resource := result.AppliedResources[0]
		run.Logs = append(run.Logs, fmt.Sprintf("[%s] Ingress %s/%s: %s",
			time.Now().Format("15:04:05"), resource.Namespace, resource.Name, resource.Operation))

		// Fetch and save ingress status to workflow node
		ingressName := resource.Name
		namespace := resource.Namespace
		if namespace == "" {
			namespace = "default"
		}

		ingressStatus, err := manifestApplier.GetIngressStatus(ctx, ingressName, namespace)
		if err != nil {
			e.logger.WithError(err).Warn("Failed to get ingress status")
		} else {
			// Update workflow node data with status
			if err := e.updateIngressNodeStatus(run.WorkflowID, node.ID, ingressStatus); err != nil {
				e.logger.WithError(err).Warn("Failed to update ingress node status in workflow")
			} else {
				run.Logs = append(run.Logs, fmt.Sprintf("[%s] Ingress status: %s",
					time.Now().Format("15:04:05"), ingressStatus.State))
			}

			// Start watching the ingress for real-time status updates (LoadBalancer IP assignment)
			watcherManager := GetResourceWatcherManager()
			restConfig := manifestApplier.GetRestConfig()

			err := watcherManager.StartWatcher(run.WorkflowID, node.ID, ingressName, namespace, "ingress", restConfig)
			if err != nil {
				e.logger.WithError(err).Warn("Failed to start ingress watcher (falling back to periodic polling)")
			} else {
				e.logger.WithFields(logrus.Fields{
					"ingress_name": ingressName,
					"namespace":    namespace,
					"node_id":      node.ID,
				}).Info("Started watching ingress for LoadBalancer IP assignment")
				run.Logs = append(run.Logs, fmt.Sprintf("[%s] Watching ingress for LoadBalancer IP assignment",
					time.Now().Format("15:04:05")))
			}
		}
	}

	// Add to output
	run.Output[node.ID] = result

	// Add log entries for each applied resource
	for _, resource := range result.AppliedResources {
		run.Logs = append(run.Logs, fmt.Sprintf("[%s] %s %s/%s: %s",
			time.Now().Format("15:04:05"), resource.Kind, resource.Namespace, resource.Name, resource.Operation))
	}

	// Save updated workflow run
	if err := e.saveWorkflowRun(run); err != nil {
		e.logger.WithError(err).Error("Failed to save updated workflow run")
	}

	return nil
}

// prepareIngressTemplateValues prepares values for ingress template rendering
func (e *WorkflowExecutor) prepareIngressTemplateValues(node *models.WorkflowNode, ingressData map[string]interface{}) map[string]interface{} {
	values := make(map[string]interface{})

	if name, ok := ingressData["name"].(string); ok {
		values["Name"] = name
	} else {
		// Fallback to node ID if name not provided
		values["Name"] = node.ID
	}

	// Copy ingress parameters
	if host, ok := ingressData["host"].(string); ok {
		values["Host"] = host
	}

	// Handle paths array (multi-path support)
	// Convert paths from MongoDB BSON types (primitive.A) to []interface{}
	var pathsSlice []interface{}
	if rawPaths, exists := ingressData["paths"]; exists {
		e.logger.WithFields(logrus.Fields{
			"node_id":     node.ID,
			"paths_type":  fmt.Sprintf("%T", rawPaths),
		}).Info("Debug: Raw paths data from ingressData")

		// Handle both primitive.A (MongoDB BSON) and []interface{} types
		switch v := rawPaths.(type) {
		case primitive.A:
			pathsSlice = []interface{}(v)
			e.logger.WithField("node_id", node.ID).Info("Debug: Converted primitive.A to []interface{}")
		case []interface{}:
			pathsSlice = v
			e.logger.WithField("node_id", node.ID).Info("Debug: Using []interface{} directly")
		}
	}

	if len(pathsSlice) > 0 {
		e.logger.WithFields(logrus.Fields{
			"node_id":     node.ID,
			"paths_count": len(pathsSlice),
		}).Info("Debug: Processing paths array")

		var templatePaths []map[string]interface{}
		for i, p := range pathsSlice {
			e.logger.WithFields(logrus.Fields{
				"node_id":       node.ID,
				"path_index":    i,
				"path_type":     fmt.Sprintf("%T", p),
			}).Info("Debug: Processing path entry")

			// Convert path data from primitive.M to map[string]interface{} if needed
			var pathData map[string]interface{}
			switch pd := p.(type) {
			case primitive.M:
				pathData = map[string]interface{}(pd)
			case map[string]interface{}:
				pathData = pd
			default:
				e.logger.WithFields(logrus.Fields{
					"node_id":    node.ID,
					"path_index": i,
					"path_type":  fmt.Sprintf("%T", p),
				}).Warn("Debug: Unable to convert path data to map")
				continue
			}

			if pathData != nil {
				templatePath := make(map[string]interface{})

				if path, ok := pathData["path"].(string); ok {
					templatePath["Path"] = path
				} else {
					templatePath["Path"] = "/"
				}

				if pathType, ok := pathData["pathType"].(string); ok {
					templatePath["PathType"] = pathType
				} else {
					templatePath["PathType"] = "Prefix"
				}

				if serviceName, ok := pathData["serviceName"].(string); ok {
					templatePath["ServiceName"] = serviceName
				}

				if servicePort, ok := pathData["servicePort"]; ok {
					templatePath["ServicePort"] = servicePort
				}

				templatePaths = append(templatePaths, templatePath)
			}
		}
		if len(templatePaths) > 0 {
			values["Paths"] = templatePaths
		}
	}

	// Backward compatibility: support old single path fields if paths array not provided
	if _, hasPaths := values["Paths"]; !hasPaths {
		if path, ok := ingressData["path"].(string); ok {
			values["Path"] = path
		}
		if pathType, ok := ingressData["pathType"].(string); ok {
			values["PathType"] = pathType
		}
		if serviceName, ok := ingressData["serviceName"].(string); ok {
			values["ServiceName"] = serviceName
		}
		if servicePort, ok := ingressData["servicePort"]; ok {
			values["ServicePort"] = servicePort
		}
	}

	if ingressClassName, ok := ingressData["ingressClassName"].(string); ok {
		values["IngressClassName"] = ingressClassName
	}

	// TLS settings
	if tlsEnabled, ok := ingressData["tlsEnabled"].(bool); ok {
		values["TLSEnabled"] = tlsEnabled
	}
	if tlsSecretName, ok := ingressData["tlsSecretName"].(string); ok {
		values["TLSSecretName"] = tlsSecretName
	}
	if tlsHosts, ok := ingressData["tlsHosts"].([]interface{}); ok {
		values["TLSHosts"] = tlsHosts
	}

	// Labels and annotations
	if labels, ok := ingressData["labels"].(map[string]interface{}); ok {
		values["Labels"] = labels
	}
	if annotations, ok := ingressData["annotations"].(map[string]interface{}); ok {
		values["Annotations"] = annotations
	}

	// Add metadata
	values["Namespace"] = "default"
	if namespace, ok := ingressData["namespace"].(string); ok {
		values["Namespace"] = namespace
	}

	return values
}

// prepareServiceTemplateValues prepares values for service template rendering
func (e *WorkflowExecutor) prepareServiceTemplateValues(node *models.WorkflowNode, serviceData map[string]interface{}) map[string]interface{} {
	values := make(map[string]interface{})

	if name, ok := serviceData["name"].(string); ok {
		values["Name"] = name
	} else {
		// Fallback to node ID if name not provided
		values["Name"] = node.ID
	}

	// Copy service parameters
	// Support both "type" and "serviceType" (frontend sends "serviceType")
	if serviceType, ok := serviceData["serviceType"].(string); ok {
		values["Type"] = serviceType
	} else if serviceType, ok := serviceData["type"].(string); ok {
		values["Type"] = serviceType
	}
	if targetApp, ok := serviceData["targetApp"].(string); ok {
		values["TargetApp"] = targetApp
	}
	if port, ok := serviceData["port"]; ok {
		values["Port"] = port
	}
	if targetPort, ok := serviceData["targetPort"]; ok {
		values["TargetPort"] = targetPort
	}
	if ports, ok := serviceData["ports"].([]interface{}); ok {
		values["Ports"] = ports
	}
	if selector, ok := serviceData["selector"].(map[string]interface{}); ok {
		values["Selector"] = selector
	}
	if sessionAffinity, ok := serviceData["sessionAffinity"].(string); ok {
		values["SessionAffinity"] = sessionAffinity
	}
	if labels, ok := serviceData["labels"].(map[string]interface{}); ok {
		values["Labels"] = labels
	}
	if annotations, ok := serviceData["annotations"].(map[string]interface{}); ok {
		values["Annotations"] = annotations
	}

	// LoadBalancer-specific fields
	if loadBalancerIP, ok := serviceData["loadBalancerIP"].(string); ok {
		values["LoadBalancerIP"] = loadBalancerIP
	}
	if sourceRanges, ok := serviceData["loadBalancerSourceRanges"].([]interface{}); ok {
		values["LoadBalancerSourceRanges"] = sourceRanges
	}
	if externalTrafficPolicy, ok := serviceData["externalTrafficPolicy"].(string); ok {
		values["ExternalTrafficPolicy"] = externalTrafficPolicy
	}

	// Add metadata
	values["Namespace"] = "default"
	if namespace, ok := serviceData["namespace"].(string); ok {
		values["Namespace"] = namespace
	}

	return values
}

// prepareTemplateValues prepares values for template rendering
func (e *WorkflowExecutor) prepareTemplateValues(node *models.WorkflowNode, deploymentData map[string]interface{}) map[string]interface{} {
	values := make(map[string]interface{})

	if name, ok := deploymentData["name"].(string); ok {
		values["Name"] = name
	} else {
		// Fallback to node ID if name not provided
		values["Name"] = node.ID
	}

	// Copy deployment parameters
	if image, ok := deploymentData["image"].(string); ok {
		values["Image"] = image
	}
	if replicas, ok := deploymentData["replicas"]; ok {
		values["Replicas"] = replicas
	}
	if port, ok := deploymentData["port"]; ok {
		values["Port"] = port
	}
	if env, ok := deploymentData["env"].(map[string]interface{}); ok {
		values["Env"] = env
	}
	if labels, ok := deploymentData["labels"].(map[string]interface{}); ok {
		values["Labels"] = labels
	}
	if resources, ok := deploymentData["resources"].(map[string]interface{}); ok {
		values["Resources"] = e.convertResources(resources)
	}

	// Handle volume mounts from ConfigMap/Secret connections
	if volumeMounts, ok := deploymentData["volumeMounts"]; ok {
		var mountsSlice []interface{}
		switch v := volumeMounts.(type) {
		case primitive.A:
			mountsSlice = []interface{}(v)
		case []interface{}:
			mountsSlice = v
		}

		if len(mountsSlice) > 0 {
			var templateMounts []map[string]interface{}
			for _, m := range mountsSlice {
				var mountData map[string]interface{}
				switch md := m.(type) {
				case primitive.M:
					mountData = map[string]interface{}(md)
				case map[string]interface{}:
					mountData = md
				default:
					continue
				}

				if mountData != nil {
					templateMount := make(map[string]interface{})
					if mountType, ok := mountData["type"].(string); ok {
						templateMount["Type"] = mountType
					}
					if name, ok := mountData["name"].(string); ok {
						templateMount["Name"] = name
					}
					if mountPath, ok := mountData["mountPath"].(string); ok {
						templateMount["MountPath"] = mountPath
					}
					templateMounts = append(templateMounts, templateMount)
				}
			}
			if len(templateMounts) > 0 {
				values["VolumeMounts"] = templateMounts
			}
		}
	}

	// Add metadata
	values["Version"] = "v1"
	values["Namespace"] = "default"
	if namespace, ok := deploymentData["namespace"].(string); ok {
		values["Namespace"] = namespace
	}

	return values
}

// convertResources converts resource specifications to template format
func (e *WorkflowExecutor) convertResources(resources map[string]interface{}) map[string]interface{} {
	converted := make(map[string]interface{})

	if requests, ok := resources["requests"].(map[string]interface{}); ok {
		requestsMap := make(map[string]interface{})
		if cpu, ok := requests["cpu"].(string); ok {
			requestsMap["CPU"] = cpu
		}
		if memory, ok := requests["memory"].(string); ok {
			requestsMap["Memory"] = memory
		}
		if len(requestsMap) > 0 {
			converted["Requests"] = requestsMap
		}
	}

	if limits, ok := resources["limits"].(map[string]interface{}); ok {
		limitsMap := make(map[string]interface{})
		if cpu, ok := limits["cpu"].(string); ok {
			limitsMap["CPU"] = cpu
		}
		if memory, ok := limits["memory"].(string); ok {
			limitsMap["Memory"] = memory
		}
		if len(limitsMap) > 0 {
			converted["Limits"] = limitsMap
		}
	}

	return converted
}

// executeConfigMapNode executes a configmap node
func (e *WorkflowExecutor) executeConfigMapNode(ctx context.Context, manifestApplier *applier.ManifestApplier, node *models.WorkflowNode, run *models.WorkflowRun) error {
	e.logger.WithFields(logrus.Fields{
		"node_id":   node.ID,
		"node_type": node.Type,
	}).Info("Executing configmap node")

	// Add log entry
	run.Logs = append(run.Logs, fmt.Sprintf("[%s] Executing configmap node: %s",
		time.Now().Format("15:04:05"), node.ID))

	// Extract configmap data from node
	configMapData := node.Data
	if configMapData == nil {
		return fmt.Errorf("invalid configmap data in node")
	}

	// Prepare template values
	templateValues := e.prepareConfigMapTemplateValues(node, configMapData)

	// Get template ID (default to core/configmap)
	templateID := "core/configmap"
	if tid, ok := configMapData["templateId"].(string); ok {
		templateID = tid
	}

	// Render template to YAML
	renderedYAML, err := e.templateEngine.RenderTemplate(templateID, templateValues)
	if err != nil {
		return fmt.Errorf("failed to render template: %w", err)
	}

	// Apply the rendered YAML directly to Kubernetes
	result, err := manifestApplier.ApplyYAML(ctx, renderedYAML)
	if err != nil {
		run.NodeStates[node.ID] = map[string]interface{}{
			"status": "failed",
			"error":  err.Error(),
		}
		// Broadcast error to SSE subscribers so frontend shows the error
		e.broadcastNodeError(run.WorkflowID, node.ID, "configmap", err.Error())
		return fmt.Errorf("failed to apply manifest: %w", err)
	}

	// Update node state
	run.NodeStates[node.ID] = map[string]interface{}{
		"status":    "completed",
		"result":    result,
		"timestamp": time.Now().Unix(),
	}

	// Log the operation performed
	if len(result.AppliedResources) > 0 {
		resource := result.AppliedResources[0]
		run.Logs = append(run.Logs, fmt.Sprintf("[%s] ConfigMap %s/%s: %s",
			time.Now().Format("15:04:05"), resource.Namespace, resource.Name, resource.Operation))

		// Update workflow node data with status
		if err := e.updateConfigMapNodeStatus(run.WorkflowID, node.ID, "created", "ConfigMap created successfully"); err != nil {
			e.logger.WithError(err).Warn("Failed to update configmap node status in workflow")
		}
	}

	// Add to output
	run.Output[node.ID] = result

	// Add log entries for each applied resource
	for _, resource := range result.AppliedResources {
		run.Logs = append(run.Logs, fmt.Sprintf("[%s] %s %s/%s: %s",
			time.Now().Format("15:04:05"), resource.Kind, resource.Namespace, resource.Name, resource.Operation))
	}

	// Save updated workflow run
	if err := e.saveWorkflowRun(run); err != nil {
		e.logger.WithError(err).Error("Failed to save updated workflow run")
	}

	return nil
}

// executeSecretNode executes a secret node
// Secret values are passed at runtime (not stored in DB) and applied directly to K8s
func (e *WorkflowExecutor) executeSecretNode(ctx context.Context, manifestApplier *applier.ManifestApplier, node *models.WorkflowNode, run *models.WorkflowRun, runtimeData map[string]interface{}) error {
	e.logger.WithFields(logrus.Fields{
		"node_id":   node.ID,
		"node_type": node.Type,
	}).Info("Executing secret node")

	// Add log entry
	run.Logs = append(run.Logs, fmt.Sprintf("[%s] Executing secret node: %s",
		time.Now().Format("15:04:05"), node.ID))

	// Extract secret data from node
	secretData := node.Data
	if secretData == nil {
		return fmt.Errorf("invalid secret data in node")
	}

	// Get secret name and namespace
	secretName, _ := secretData["name"].(string)
	if secretName == "" {
		secretName = node.ID
	}
	namespace := "default"
	if ns, ok := secretData["namespace"].(string); ok && ns != "" {
		namespace = ns
	}

	// Get secret type
	secretType := "Opaque"
	if st, ok := secretData["secretType"].(string); ok && st != "" {
		secretType = st
	}

	// Get runtime secret values from trigger data
	// Structure: runtimeData["secrets"][nodeID] = { key: value }
	var secretValues map[string]interface{}
	if secrets, ok := runtimeData["secrets"].(map[string]interface{}); ok {
		if nodeSecrets, ok := secrets[node.ID].(map[string]interface{}); ok {
			secretValues = nodeSecrets
		}
	}

	// If no runtime secrets provided, check if secret already exists
	if len(secretValues) == 0 {
		secretExists, err := manifestApplier.CheckSecretExists(ctx, secretName, namespace)
		if err != nil {
			e.logger.WithError(err).Warn("Failed to check if secret exists")
		}

		if secretExists {
			run.Logs = append(run.Logs, fmt.Sprintf("[%s] Secret %s/%s already exists (no new values provided)",
				time.Now().Format("15:04:05"), namespace, secretName))

			run.NodeStates[node.ID] = map[string]interface{}{
				"status":    "completed",
				"timestamp": time.Now().Unix(),
				"message":   "Secret exists",
			}

			if err := e.updateSecretNodeStatus(run.WorkflowID, node.ID, "created", "Secret exists in Kubernetes", true); err != nil {
				e.logger.WithError(err).Warn("Failed to update secret node status in workflow")
			}
		} else {
			run.Logs = append(run.Logs, fmt.Sprintf("[%s] Warning: Secret %s/%s not found and no values provided",
				time.Now().Format("15:04:05"), namespace, secretName))

			run.NodeStates[node.ID] = map[string]interface{}{
				"status":    "warning",
				"timestamp": time.Now().Unix(),
				"message":   "Secret not found - provide values in UI",
			}

			if err := e.updateSecretNodeStatus(run.WorkflowID, node.ID, "pending", "Secret not created - no values provided", false); err != nil {
				e.logger.WithError(err).Warn("Failed to update secret node status in workflow")
			}
		}

		run.Output[node.ID] = map[string]interface{}{
			"name":      secretName,
			"namespace": namespace,
			"exists":    secretExists,
		}

		if err := e.saveWorkflowRun(run); err != nil {
			e.logger.WithError(err).Error("Failed to save updated workflow run")
		}

		return nil
	}

	// Prepare template values with runtime secret data
	templateValues := e.prepareSecretTemplateValues(node, secretData, secretValues, secretType)

	// Get template ID
	templateID := "core/secret"
	if tid, ok := secretData["templateId"].(string); ok && tid != "" {
		templateID = tid
	}

	// Render the secret template
	renderedYAML, err := e.templateEngine.RenderTemplate(templateID, templateValues)
	if err != nil {
		e.broadcastNodeError(run.WorkflowID, node.ID, "secret", fmt.Sprintf("Failed to render secret template: %v", err))
		return fmt.Errorf("failed to render secret template: %w", err)
	}

	run.Logs = append(run.Logs, fmt.Sprintf("[%s] Creating secret %s/%s",
		time.Now().Format("15:04:05"), namespace, secretName))

	e.logger.WithFields(logrus.Fields{
		"name":      secretName,
		"namespace": namespace,
	}).Info("Updating resource", "kind", "Secret")

	// Apply the secret to Kubernetes
	result, err := manifestApplier.ApplyYAML(ctx, renderedYAML)
	if err != nil {
		e.broadcastNodeError(run.WorkflowID, node.ID, "secret", fmt.Sprintf("Failed to apply secret: %v", err))
		return fmt.Errorf("failed to apply secret: %w", err)
	}

	// Get operation from first applied resource
	operation := "created"
	if len(result.AppliedResources) > 0 {
		operation = result.AppliedResources[0].Operation
	}

	// Update node state
	run.NodeStates[node.ID] = map[string]interface{}{
		"status":    "completed",
		"timestamp": time.Now().Unix(),
		"message":   fmt.Sprintf("Secret %s", operation),
	}

	// Broadcast status update via SSE
	e.broadcastSecretNodeUpdate(run.WorkflowID, node.ID, "created", fmt.Sprintf("Secret %s successfully", operation))

	// Update workflow node data with status
	if err := e.updateSecretNodeStatus(run.WorkflowID, node.ID, "created", fmt.Sprintf("Secret %s successfully", operation), true); err != nil {
		e.logger.WithError(err).Warn("Failed to update secret node status in workflow")
	}

	run.Logs = append(run.Logs, fmt.Sprintf("[%s] Secret %s/%s %s successfully",
		time.Now().Format("15:04:05"), namespace, secretName, operation))

	// Add to output
	run.Output[node.ID] = map[string]interface{}{
		"name":      secretName,
		"namespace": namespace,
		"operation": operation,
	}

	// Save updated workflow run
	if err := e.saveWorkflowRun(run); err != nil {
		e.logger.WithError(err).Error("Failed to save updated workflow run")
	}

	return nil
}

// prepareSecretTemplateValues prepares values for secret template rendering
func (e *WorkflowExecutor) prepareSecretTemplateValues(node *models.WorkflowNode, secretData map[string]interface{}, secretValues map[string]interface{}, secretType string) map[string]interface{} {
	values := make(map[string]interface{})

	if name, ok := secretData["name"].(string); ok {
		values["Name"] = name
	} else {
		values["Name"] = node.ID
	}

	// Set secret type
	values["SecretType"] = secretType

	// Handle secret key-value pairs from runtime data
	// Filter out keys that start with _new_ (empty placeholder keys that weren't renamed)
	filteredValues := make(map[string]interface{})
	skippedPlaceholders := 0
	for key, value := range secretValues {
		// Skip placeholder keys that weren't renamed by the user
		if len(key) > 5 && key[:5] == "_new_" {
			skippedPlaceholders++
			continue
		}
		filteredValues[key] = value
	}
	if skippedPlaceholders > 0 {
		e.logger.WithFields(logrus.Fields{
			"secret_name":          values["Name"],
			"skipped_placeholders": skippedPlaceholders,
		}).Warn("Skipped placeholder secret keys that were not renamed by user")
	}
	values["Data"] = filteredValues

	// Add namespace
	values["Namespace"] = "default"
	if namespace, ok := secretData["namespace"].(string); ok && namespace != "" {
		values["Namespace"] = namespace
	}

	return values
}

// broadcastSecretNodeUpdate broadcasts a secret node status update via SSE
func (e *WorkflowExecutor) broadcastSecretNodeUpdate(workflowID primitive.ObjectID, nodeID string, state string, message string) {
	broadcaster := GetSSEBroadcaster()
	broadcaster.Publish(StreamEvent{
		Type:      "workflow",
		StreamKey: fmt.Sprintf("workflow:%s", workflowID.Hex()),
		EventType: "node_update",
		Data: map[string]interface{}{
			"node_id": nodeID,
			"type":    "secret",
			"status": map[string]interface{}{
				"state":   state,
				"message": message,
			},
		},
	})
}

// prepareConfigMapTemplateValues prepares values for configmap template rendering
func (e *WorkflowExecutor) prepareConfigMapTemplateValues(node *models.WorkflowNode, configMapData map[string]interface{}) map[string]interface{} {
	values := make(map[string]interface{})

	if name, ok := configMapData["name"].(string); ok {
		values["Name"] = name
	} else {
		values["Name"] = node.ID
	}

	// Handle data key-value pairs
	if data, ok := configMapData["data"]; ok {
		var dataMap map[string]interface{}
		switch d := data.(type) {
		case primitive.M:
			dataMap = map[string]interface{}(d)
		case map[string]interface{}:
			dataMap = d
		}
		if dataMap != nil {
			values["Data"] = dataMap
		}
	}

	// Add metadata
	values["Namespace"] = "default"
	if namespace, ok := configMapData["namespace"].(string); ok {
		values["Namespace"] = namespace
	}

	return values
}

// updateConfigMapNodeStatus updates a workflow node's _status field with configmap status
func (e *WorkflowExecutor) updateConfigMapNodeStatus(workflowID primitive.ObjectID, nodeID string, state string, message string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get the current workflow
	var workflow models.Workflow
	filter := bson.M{"_id": workflowID}
	err := database.WorkflowColl.FindOne(ctx, filter).Decode(&workflow)
	if err != nil {
		return fmt.Errorf("failed to get workflow: %w", err)
	}

	// Find and update the node with status
	for i, node := range workflow.Nodes {
		if node.ID == nodeID {
			if workflow.Nodes[i].Data == nil {
				workflow.Nodes[i].Data = make(map[string]interface{})
			}
			workflow.Nodes[i].Data["_status"] = map[string]interface{}{
				"state":   state,
				"message": message,
			}
			break
		}
	}

	// Update the workflow with new nodes
	update := bson.M{
		"$set": bson.M{
			"nodes":      workflow.Nodes,
			"updated_at": time.Now(),
		},
	}

	_, err = database.WorkflowColl.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update workflow: %w", err)
	}

	// Publish status update event to SSE subscribers
	broadcaster := GetSSEBroadcaster()
	broadcaster.Publish(StreamEvent{
		Type:      "workflow",
		StreamKey: fmt.Sprintf("workflow:%s", workflowID.Hex()),
		EventType: "node_update",
		Data: map[string]interface{}{
			"node_id": nodeID,
			"type":    "configmap",
			"status": map[string]interface{}{
				"state":   state,
				"message": message,
			},
		},
	})

	return nil
}

// updateSecretNodeStatus updates a workflow node's _status field with secret status
func (e *WorkflowExecutor) updateSecretNodeStatus(workflowID primitive.ObjectID, nodeID string, state string, message string, secretCreated bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get the current workflow
	var workflow models.Workflow
	filter := bson.M{"_id": workflowID}
	err := database.WorkflowColl.FindOne(ctx, filter).Decode(&workflow)
	if err != nil {
		return fmt.Errorf("failed to get workflow: %w", err)
	}

	// Find and update the node with status
	for i, node := range workflow.Nodes {
		if node.ID == nodeID {
			if workflow.Nodes[i].Data == nil {
				workflow.Nodes[i].Data = make(map[string]interface{})
			}
			workflow.Nodes[i].Data["_status"] = map[string]interface{}{
				"state":   state,
				"message": message,
			}
			workflow.Nodes[i].Data["_secretCreated"] = secretCreated
			break
		}
	}

	// Update the workflow with new nodes
	update := bson.M{
		"$set": bson.M{
			"nodes":      workflow.Nodes,
			"updated_at": time.Now(),
		},
	}

	_, err = database.WorkflowColl.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update workflow: %w", err)
	}

	// Publish status update event to SSE subscribers
	broadcaster := GetSSEBroadcaster()
	broadcaster.Publish(StreamEvent{
		Type:      "workflow",
		StreamKey: fmt.Sprintf("workflow:%s", workflowID.Hex()),
		EventType: "node_update",
		Data: map[string]interface{}{
			"node_id": nodeID,
			"type":    "secret",
			"status": map[string]interface{}{
				"state":         state,
				"message":       message,
				"secretCreated": secretCreated,
			},
		},
	})

	return nil
}

// sanitizeErrorForBroadcast removes potentially sensitive information from error messages
// before sending them to the frontend via SSE. This prevents leaking internal paths,
// credentials, tokens, or other sensitive details to clients.
func sanitizeErrorForBroadcast(errorMessage string) string {
	// List of patterns that might contain sensitive info
	sensitivePatterns := []string{
		"bearer ", "token=", "password=", "secret=", "key=",
		"authorization:", "credential", "/home/", "/Users/",
		"/etc/", "/var/", "mongodb://", "postgres://", "mysql://",
	}

	sanitized := errorMessage
	lowerMsg := strings.ToLower(errorMessage)

	for _, pattern := range sensitivePatterns {
		if strings.Contains(lowerMsg, pattern) {
			// Return a generic message if sensitive content detected
			return "An error occurred while processing this node. Check server logs for details."
		}
	}

	// Truncate very long error messages that might contain stack traces
	if len(sanitized) > 500 {
		sanitized = sanitized[:500] + "... (truncated)"
	}

	return sanitized
}

// broadcastNodeError sends an error status update to SSE subscribers when a node fails.
// Error messages are sanitized to prevent leaking sensitive information to the frontend.
func (e *WorkflowExecutor) broadcastNodeError(workflowID primitive.ObjectID, nodeID string, nodeType string, errorMessage string) {
	// Sanitize error message before broadcasting to prevent sensitive info leakage
	sanitizedMessage := sanitizeErrorForBroadcast(errorMessage)

	broadcaster := GetSSEBroadcaster()
	broadcaster.Publish(StreamEvent{
		Type:      "workflow",
		StreamKey: fmt.Sprintf("workflow:%s", workflowID.Hex()),
		EventType: "node_update",
		Data: map[string]interface{}{
			"node_id": nodeID,
			"type":    nodeType,
			"status": map[string]interface{}{
				"state":   "error",
				"message": sanitizedMessage,
			},
		},
	})

	// Log full error for debugging (not sanitized)
	e.logger.WithFields(logrus.Fields{
		"workflow_id": workflowID.Hex(),
		"node_id":     nodeID,
		"node_type":   nodeType,
		"error":       errorMessage,
	}).Info("Broadcasted node error to SSE subscribers")
}

// getClusterForWorkflow retrieves cluster for workflow
func (e *WorkflowExecutor) getClusterForWorkflow(ctx context.Context, clusterID string, userID primitive.ObjectID) (*models.Cluster, error) {
	clusterRepo := e.clusterService.clusterRepo

	// Get clusters accessible to this user (owned + shared)
	userClusters, err := clusterRepo.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user clusters: %w", err)
	}

	// Check user's own clusters first
	for _, cluster := range userClusters {
		if cluster.Name == clusterID {
			return cluster, nil
		}
	}

	// Check if user has access through sharing (organization/team level)
	// This includes clusters shared with the user or their organization
	allClusters, err := clusterRepo.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get clusters: %w", err)
	}

	for _, cluster := range allClusters {
		// Check if cluster matches and user has access
		if cluster.Name == clusterID {
			// Check if cluster is shared with this user
			for _, sharedUserID := range cluster.SharedWith {
				if sharedUserID == userID {
					return cluster, nil
				}
			}
			// Check if cluster belongs to same organization (future: check org membership)
			// For now, allow access if cluster exists (team environment)
			return cluster, nil
		}
	}

	// If no specific cluster found, look for user's default
	defaultCluster, err := clusterRepo.GetDefault(ctx, userID)
	if err == nil && defaultCluster != nil {
		return defaultCluster, nil
	}

	return nil, fmt.Errorf("no accessible cluster found")
}

// saveWorkflowRun saves or updates workflow run in database
func (e *WorkflowExecutor) saveWorkflowRun(run *models.WorkflowRun) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if run.ID.IsZero() {
		// Insert new run
		result, err := database.WorkflowRunColl.InsertOne(ctx, run)
		if err != nil {
			return err
		}
		run.ID = result.InsertedID.(primitive.ObjectID)
	} else {
		// Update existing run
		filter := bson.M{"_id": run.ID}
		update := bson.M{"$set": run}
		_, err := database.WorkflowRunColl.UpdateOne(ctx, filter, update)
		if err != nil {
			return err
		}
	}

	return nil
}

// updateWorkflowRunStatus updates workflow run status
func (e *WorkflowExecutor) updateWorkflowRunStatus(run *models.WorkflowRun, status models.WorkflowRunStatus, message string) {
	run.Status = status
	if status == models.WorkflowRunStatusFailed {
		run.Error = message
	}
	run.Logs = append(run.Logs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), message))

	if err := e.saveWorkflowRun(run); err != nil {
		e.logger.WithError(err).Error("Failed to update workflow run status")
	}
}

// updateWorkflowStats updates workflow execution statistics
func (e *WorkflowExecutor) updateWorkflowStats(workflowID primitive.ObjectID, success bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	update := bson.M{
		"$inc": bson.M{"run_count": 1},
		"$set": bson.M{
			"last_run_at": time.Now(),
		},
	}

	if success {
		update["$inc"].(bson.M)["success_count"] = 1
	} else {
		update["$inc"].(bson.M)["failure_count"] = 1
	}

	filter := bson.M{"_id": workflowID}
	if _, err := database.WorkflowColl.UpdateOne(ctx, filter, update); err != nil {
		e.logger.WithError(err).Error("Failed to update workflow statistics")
	}
}

// updateDeploymentNodeStatus updates a workflow node's _status field with deployment status
func (e *WorkflowExecutor) updateDeploymentNodeStatus(workflowID primitive.ObjectID, nodeID string, status *applier.DeploymentStatus) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get the current workflow
	var workflow models.Workflow
	filter := bson.M{"_id": workflowID}
	err := database.WorkflowColl.FindOne(ctx, filter).Decode(&workflow)
	if err != nil {
		return fmt.Errorf("failed to get workflow: %w", err)
	}

	// Find and update the node with status
	for i, node := range workflow.Nodes {
		if node.ID == nodeID {
			if workflow.Nodes[i].Data == nil {
				workflow.Nodes[i].Data = make(map[string]interface{})
			}
			workflow.Nodes[i].Data["_status"] = map[string]interface{}{
				"state":         status.State,
				"replicas":      status.Replicas,
				"readyReplicas": status.ReadyReplicas,
				"message":       status.Message,
			}
			break
		}
	}

	// Update the workflow with new nodes
	update := bson.M{
		"$set": bson.M{
			"nodes":      workflow.Nodes,
			"updated_at": time.Now(),
		},
	}

	_, err = database.WorkflowColl.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update workflow: %w", err)
	}

	e.logger.WithFields(logrus.Fields{
		"workflow_id":    workflowID.Hex(),
		"node_id":        nodeID,
		"state":          status.State,
		"replicas":       status.Replicas,
		"ready_replicas": status.ReadyReplicas,
	}).Info("Updated deployment node status in workflow")

	// Publish status update event to SSE subscribers
	broadcaster := GetSSEBroadcaster()
	broadcaster.Publish(StreamEvent{
		Type:      "workflow",
		StreamKey: fmt.Sprintf("workflow:%s", workflowID.Hex()),
		EventType: "node_update",
		Data: map[string]interface{}{
			"node_id": nodeID,
			"type":    "deployment",
			"status": map[string]interface{}{
				"state":         status.State,
				"replicas":      status.Replicas,
				"readyReplicas": status.ReadyReplicas,
				"message":       status.Message,
			},
		},
	})

	return nil
}

// updateNodeStatus updates a workflow node's _status field with service status
func (e *WorkflowExecutor) updateNodeStatus(workflowID primitive.ObjectID, nodeID string, status *applier.ServiceStatus) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get the current workflow
	var workflow models.Workflow
	filter := bson.M{"_id": workflowID}
	err := database.WorkflowColl.FindOne(ctx, filter).Decode(&workflow)
	if err != nil {
		return fmt.Errorf("failed to get workflow: %w", err)
	}

	// Find and update the node with status
	for i, node := range workflow.Nodes {
		if node.ID == nodeID {
			if workflow.Nodes[i].Data == nil {
				workflow.Nodes[i].Data = make(map[string]interface{})
			}
			workflow.Nodes[i].Data["_status"] = map[string]interface{}{
				"state":      status.State,
				"clusterIP":  status.ClusterIP,
				"externalIP": status.ExternalIP,
				"nodePort":   status.NodePort,
				"message":    status.Message,
			}
			break
		}
	}

	// Update the workflow with new nodes
	update := bson.M{
		"$set": bson.M{
			"nodes":      workflow.Nodes,
			"updated_at": time.Now(),
		},
	}

	_, err = database.WorkflowColl.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update workflow: %w", err)
	}

	e.logger.WithFields(logrus.Fields{
		"workflow_id": workflowID.Hex(),
		"node_id":     nodeID,
		"state":       status.State,
		"cluster_ip":  status.ClusterIP,
	}).Info("Updated node status in workflow")

	// Publish status update event to SSE subscribers
	broadcaster := GetSSEBroadcaster()
	broadcaster.Publish(StreamEvent{
		Type:      "workflow",
		StreamKey: fmt.Sprintf("workflow:%s", workflowID.Hex()),
		EventType: "node_update",
		Data: map[string]interface{}{
			"node_id": nodeID,
			"type":    "service",
			"status": map[string]interface{}{
				"state":      status.State,
				"clusterIP":  status.ClusterIP,
				"externalIP": status.ExternalIP,
				"nodePort":   status.NodePort,
				"message":    status.Message,
			},
		},
	})

	return nil
}

// updateIngressNodeStatus updates a workflow node's _status field with ingress status
func (e *WorkflowExecutor) updateIngressNodeStatus(workflowID primitive.ObjectID, nodeID string, status *applier.IngressStatus) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get the current workflow
	var workflow models.Workflow
	filter := bson.M{"_id": workflowID}
	err := database.WorkflowColl.FindOne(ctx, filter).Decode(&workflow)
	if err != nil {
		return fmt.Errorf("failed to get workflow: %w", err)
	}

	// Find and update the node with status
	for i, node := range workflow.Nodes {
		if node.ID == nodeID {
			if workflow.Nodes[i].Data == nil {
				workflow.Nodes[i].Data = make(map[string]interface{})
			}
			workflow.Nodes[i].Data["_status"] = map[string]interface{}{
				"state":                status.State,
				"loadBalancerIP":       status.LoadBalancerIP,
				"loadBalancerHostname": status.LoadBalancerHostname,
				"rulesCount":           status.RulesCount,
				"message":              status.Message,
			}
			break
		}
	}

	// Update the workflow with new nodes
	update := bson.M{
		"$set": bson.M{
			"nodes":      workflow.Nodes,
			"updated_at": time.Now(),
		},
	}

	_, err = database.WorkflowColl.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update workflow: %w", err)
	}

	e.logger.WithFields(logrus.Fields{
		"workflow_id":      workflowID.Hex(),
		"node_id":          nodeID,
		"state":            status.State,
		"load_balancer_ip": status.LoadBalancerIP,
	}).Info("Updated ingress node status in workflow")

	// Publish status update event to SSE subscribers
	broadcaster := GetSSEBroadcaster()
	broadcaster.Publish(StreamEvent{
		Type:      "workflow",
		StreamKey: fmt.Sprintf("workflow:%s", workflowID.Hex()),
		EventType: "node_update",
		Data: map[string]interface{}{
			"node_id": nodeID,
			"type":    "ingress",
			"status": map[string]interface{}{
				"state":                status.State,
				"loadBalancerIP":       status.LoadBalancerIP,
				"loadBalancerHostname": status.LoadBalancerHostname,
				"rulesCount":           status.RulesCount,
				"message":              status.Message,
			},
		},
	})

	return nil
}

// SyncWorkflowStatuses updates the status of all workflow nodes based on current K8s state
func (e *WorkflowExecutor) SyncWorkflowStatuses(ctx context.Context, userID primitive.ObjectID, cluster *models.Cluster) error {
	// Get all published workflows for this user and cluster (ClusterID in workflow is the cluster Name)
	workflows, err := GetWorkflowsByUserAndCluster(userID, cluster.Name)
	if err != nil {
		return fmt.Errorf("failed to get workflows: %w", err)
	}

	e.logger.WithFields(logrus.Fields{
		"user_id":        userID.Hex(),
		"cluster_name":   cluster.Name,
		"workflow_count": len(workflows),
	}).Debug("Syncing workflow statuses")

	auth := e.clusterService.ClusterToAuthConfig(cluster)
	config, err := auth.BuildRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to build K8s config: %w", err)
	}

	manifestApplier, err := applier.NewManifestApplier(config, "default")
	if err != nil {
		return fmt.Errorf("failed to create manifest applier: %w", err)
	}

	for _, workflow := range workflows {
		// Only sync published workflows that have been run
		if workflow.Status != models.WorkflowStatusPublished || workflow.RunCount == 0 {
			e.logger.WithFields(logrus.Fields{
				"workflow_id": workflow.ID.Hex(),
				"status":      workflow.Status,
				"run_count":   workflow.RunCount,
			}).Debug("Skipping workflow - not published or not run")
			continue
		}

		e.logger.WithFields(logrus.Fields{
			"workflow_id": workflow.ID.Hex(),
			"name":        workflow.Name,
			"node_count":  len(workflow.Nodes),
		}).Info("Syncing workflow status")

		updated := false
		for i, node := range workflow.Nodes {
			nodeType, _ := node.Data["templateId"].(string)
			name, _ := node.Data["name"].(string)
			namespace, _ := node.Data["namespace"].(string)
			if namespace == "" {
				namespace = "default"
			}

			if nodeType == "core/deployment" {
				status, err := manifestApplier.GetDeploymentStatus(ctx, name, namespace)
				if err != nil {
					continue // Resource might not exist
				}
				workflow.Nodes[i].Data["_status"] = map[string]interface{}{
					"state":         status.State,
					"replicas":      status.Replicas,
					"readyReplicas": status.ReadyReplicas,
					"message":       status.Message,
				}
				updated = true
			} else if nodeType == "core/service" {
				status, err := manifestApplier.GetServiceStatus(ctx, name, namespace)
				if err != nil {
					continue // Resource might not exist
				}
				workflow.Nodes[i].Data["_status"] = map[string]interface{}{
					"state":      status.State,
					"clusterIP":  status.ClusterIP,
					"externalIP": status.ExternalIP,
					"nodePort":   status.NodePort,
					"message":    status.Message,
				}
				updated = true
			} else if nodeType == "core/ingress" {
				status, err := manifestApplier.GetIngressStatus(ctx, name, namespace)
				if err != nil {
					continue // Resource might not exist
				}
				workflow.Nodes[i].Data["_status"] = map[string]interface{}{
					"state":                status.State,
					"loadBalancerIP":       status.LoadBalancerIP,
					"loadBalancerHostname": status.LoadBalancerHostname,
					"rulesCount":           status.RulesCount,
					"message":              status.Message,
				}
				updated = true
			}
		}

		if updated {
			// Save updated workflow
			filter := bson.M{"_id": workflow.ID}
			update := bson.M{
				"$set": bson.M{
					"nodes":      workflow.Nodes,
					"updated_at": time.Now(),
				},
			}
			_, err = database.WorkflowColl.UpdateOne(ctx, filter, update)
			if err != nil {
				e.logger.WithError(err).Warnf("Failed to update workflow %s status", workflow.ID.Hex())
			} else {
				e.logger.WithFields(logrus.Fields{
					"workflow_id": workflow.ID.Hex(),
					"node_count":  len(workflow.Nodes),
				}).Info("Workflow status updated, broadcasting to SSE subscribers")

				// Publish workflow sync event to SSE subscribers
				broadcaster := GetSSEBroadcaster()
				broadcaster.Publish(StreamEvent{
					Type:      "workflow",
					StreamKey: fmt.Sprintf("workflow:%s", workflow.ID.Hex()),
					EventType: "workflow_sync",
					Data: map[string]interface{}{
						"nodes": workflow.Nodes,
					},
				})
			}
		} else {
			e.logger.WithField("workflow_id", workflow.ID.Hex()).Debug("No status changes detected for workflow")
		}
	}

	return nil
}

// CleanupWorkflowResources deletes all Kubernetes resources created by a workflow
func (e *WorkflowExecutor) CleanupWorkflowResources(ctx context.Context, workflow *models.Workflow, userID primitive.ObjectID) error {
	e.logger.WithFields(logrus.Fields{
		"workflow_id":   workflow.ID.Hex(),
		"workflow_name": workflow.Name,
		"node_count":    len(workflow.Nodes),
	}).Info("Starting cleanup of workflow K8s resources")

	// Stop all watchers for this workflow
	watcherManager := GetResourceWatcherManager()
	watcherManager.StopWorkflowWatchers(workflow.ID)

	// Get cluster for this workflow
	cluster, err := e.getClusterForWorkflow(ctx, workflow.ClusterID, userID)
	if err != nil {
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	auth := e.clusterService.ClusterToAuthConfig(cluster)
	config, err := auth.BuildRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to build REST config: %w", err)
	}

	manifestApplier, err := applier.NewManifestApplier(config, "default")
	if err != nil {
		return fmt.Errorf("failed to create manifest applier: %w", err)
	}

	var cleanupErrors []string

	// Delete resources in reverse order (ingress first, then services, then deployments)
	// This ensures ingress is removed before services, and services before deployments
	for _, node := range workflow.Nodes {
		if node.Type == "ingress" {
			if err := e.deleteIngressNode(ctx, manifestApplier, &node); err != nil {
				e.logger.WithError(err).WithField("node_id", node.ID).Warn("Failed to delete ingress")
				cleanupErrors = append(cleanupErrors, err.Error())
			}
		}
	}

	for _, node := range workflow.Nodes {
		if node.Type == "service" {
			if err := e.deleteServiceNode(ctx, manifestApplier, &node); err != nil {
				e.logger.WithError(err).WithField("node_id", node.ID).Warn("Failed to delete service")
				cleanupErrors = append(cleanupErrors, err.Error())
			}
		}
	}

	for _, node := range workflow.Nodes {
		if node.Type == "deployment" {
			if err := e.deleteDeploymentNode(ctx, manifestApplier, &node); err != nil {
				e.logger.WithError(err).WithField("node_id", node.ID).Warn("Failed to delete deployment")
				cleanupErrors = append(cleanupErrors, err.Error())
			}
		}
	}

	// Delete ConfigMaps last (after deployments that use them)
	// Note: We don't delete Secrets as they are managed externally (pass-through)
	for _, node := range workflow.Nodes {
		if node.Type == "configmap" {
			if err := e.deleteConfigMapNode(ctx, manifestApplier, &node); err != nil {
				e.logger.WithError(err).WithField("node_id", node.ID).Warn("Failed to delete configmap")
				cleanupErrors = append(cleanupErrors, err.Error())
			}
		}
	}

	if len(cleanupErrors) > 0 {
		return fmt.Errorf("cleanup completed with errors: %v", cleanupErrors)
	}

	e.logger.WithField("workflow_id", workflow.ID.Hex()).Info("Workflow K8s resources cleanup completed")
	return nil
}

// deleteConfigMapNode deletes a Kubernetes ConfigMap created by a configmap node
func (e *WorkflowExecutor) deleteConfigMapNode(ctx context.Context, manifestApplier *applier.ManifestApplier, node *models.WorkflowNode) error {
	if node.Data == nil {
		return nil
	}

	// Get configmap name from node data
	name, ok := node.Data["name"].(string)
	if !ok || name == "" {
		name = node.ID // Fallback to node ID
	}

	// Get namespace from node data
	namespace := "default"
	if ns, ok := node.Data["namespace"].(string); ok && ns != "" {
		namespace = ns
	}

	e.logger.WithFields(logrus.Fields{
		"configmap": name,
		"namespace": namespace,
		"node_id":   node.ID,
	}).Info("Deleting configmap from workflow cleanup")

	return manifestApplier.DeleteConfigMap(ctx, name, namespace)
}

// deleteDeploymentNode deletes a Kubernetes Deployment created by a deployment node
func (e *WorkflowExecutor) deleteDeploymentNode(ctx context.Context, manifestApplier *applier.ManifestApplier, node *models.WorkflowNode) error {
	if node.Data == nil {
		return nil
	}

	// Get deployment name from node data
	name, ok := node.Data["name"].(string)
	if !ok || name == "" {
		name = node.ID // Fallback to node ID
	}

	// Get namespace from node data
	namespace := "default"
	if ns, ok := node.Data["namespace"].(string); ok && ns != "" {
		namespace = ns
	}

	e.logger.WithFields(logrus.Fields{
		"deployment": name,
		"namespace":  namespace,
		"node_id":    node.ID,
	}).Info("Deleting deployment from workflow cleanup")

	return manifestApplier.DeleteDeployment(ctx, name, namespace)
}

// deleteServiceNode deletes a Kubernetes Service created by a service node
func (e *WorkflowExecutor) deleteServiceNode(ctx context.Context, manifestApplier *applier.ManifestApplier, node *models.WorkflowNode) error {
	if node.Data == nil {
		return nil
	}

	// Get service name from node data
	name, ok := node.Data["name"].(string)
	if !ok || name == "" {
		name = node.ID // Fallback to node ID
	}

	// Get namespace from node data
	namespace := "default"
	if ns, ok := node.Data["namespace"].(string); ok && ns != "" {
		namespace = ns
	}

	e.logger.WithFields(logrus.Fields{
		"service":   name,
		"namespace": namespace,
		"node_id":   node.ID,
	}).Info("Deleting service from workflow cleanup")

	return manifestApplier.DeleteService(ctx, name, namespace)
}

// deleteIngressNode deletes a Kubernetes Ingress created by an ingress node
func (e *WorkflowExecutor) deleteIngressNode(ctx context.Context, manifestApplier *applier.ManifestApplier, node *models.WorkflowNode) error {
	if node.Data == nil {
		return nil
	}

	// Get ingress name from node data
	name, ok := node.Data["name"].(string)
	if !ok || name == "" {
		name = node.ID // Fallback to node ID
	}

	// Get namespace from node data
	namespace := "default"
	if ns, ok := node.Data["namespace"].(string); ok && ns != "" {
		namespace = ns
	}

	e.logger.WithFields(logrus.Fields{
		"ingress":   name,
		"namespace": namespace,
		"node_id":   node.ID,
	}).Info("Deleting ingress from workflow cleanup")

	return manifestApplier.DeleteIngress(ctx, name, namespace)
}

// CleanupDeletedNodes deletes Kubernetes resources for specific nodes that were removed from a workflow
func (e *WorkflowExecutor) CleanupDeletedNodes(ctx context.Context, workflow *models.Workflow, deletedNodes []models.WorkflowNode, userID primitive.ObjectID) error {
	if len(deletedNodes) == 0 {
		return nil
	}

	e.logger.WithFields(logrus.Fields{
		"workflow_id":     workflow.ID.Hex(),
		"workflow_name":   workflow.Name,
		"deleted_count":   len(deletedNodes),
	}).Info("Starting cleanup of deleted workflow nodes")

	// Get cluster for this workflow
	cluster, err := e.getClusterForWorkflow(ctx, workflow.ClusterID, userID)
	if err != nil {
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	auth := e.clusterService.ClusterToAuthConfig(cluster)
	config, err := auth.BuildRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to build REST config: %w", err)
	}

	manifestApplier, err := applier.NewManifestApplier(config, "default")
	if err != nil {
		return fmt.Errorf("failed to create manifest applier: %w", err)
	}

	var cleanupErrors []string

	// Delete in reverse order: ingress first, then services, then deployments
	for _, node := range deletedNodes {
		if node.Type == "ingress" {
			if err := e.deleteIngressNode(ctx, manifestApplier, &node); err != nil {
				e.logger.WithError(err).WithField("node_id", node.ID).Warn("Failed to delete ingress")
				cleanupErrors = append(cleanupErrors, err.Error())
			}
		}
	}

	for _, node := range deletedNodes {
		if node.Type == "service" {
			if err := e.deleteServiceNode(ctx, manifestApplier, &node); err != nil {
				e.logger.WithError(err).WithField("node_id", node.ID).Warn("Failed to delete service")
				cleanupErrors = append(cleanupErrors, err.Error())
			}
		}
	}

	for _, node := range deletedNodes {
		if node.Type == "deployment" {
			if err := e.deleteDeploymentNode(ctx, manifestApplier, &node); err != nil {
				e.logger.WithError(err).WithField("node_id", node.ID).Warn("Failed to delete deployment")
				cleanupErrors = append(cleanupErrors, err.Error())
			}
		}
	}

	// Delete configmaps last (after deployments)
	// Note: We don't delete Secrets as they are managed externally (pass-through)
	for _, node := range deletedNodes {
		if node.Type == "configmap" {
			if err := e.deleteConfigMapNode(ctx, manifestApplier, &node); err != nil {
				e.logger.WithError(err).WithField("node_id", node.ID).Warn("Failed to delete configmap")
				cleanupErrors = append(cleanupErrors, err.Error())
			}
		}
	}

	if len(cleanupErrors) > 0 {
		return fmt.Errorf("cleanup completed with errors: %v", cleanupErrors)
	}

	e.logger.WithField("workflow_id", workflow.ID.Hex()).Info("Deleted nodes cleanup completed")
	return nil
}