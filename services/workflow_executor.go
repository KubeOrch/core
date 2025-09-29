package services

import (
	"context"
	"fmt"
	"path/filepath"
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

// ExecuteWorkflow executes a workflow
func (e *WorkflowExecutor) ExecuteWorkflow(ctx context.Context, workflowID primitive.ObjectID, userID primitive.ObjectID) (*models.WorkflowRun, error) {
	// Get workflow
	workflow, err := GetWorkflowByID(workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow: %w", err)
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

	auth := e.clusterService.clusterToAuthConfig(cluster)
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

	for _, node := range workflow.Nodes {
		if node.Type == "deployment" {
			if err := e.executeDeploymentNode(ctx, manifestApplier, &node, workflowRun); err != nil {
				e.updateWorkflowRunStatus(workflowRun, models.WorkflowRunStatusFailed, err.Error())
				e.updateWorkflowStats(workflowID, false)
				return workflowRun, fmt.Errorf("failed to execute node %s: %w", node.ID, err)
			}
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