package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/middleware"
	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/pkg/applier"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktypes "k8s.io/apimachinery/pkg/types"
	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const maxErrorDetailsLength = 200

// GetNodeDiagnosticsHandler returns diagnostics for a workflow node
// GET /v1/api/workflows/:id/nodes/:nodeId/diagnostics
func GetNodeDiagnosticsHandler(c *gin.Context) {
	workflowID := c.Param("id")
	nodeID := c.Param("nodeId")
	logger := logrus.WithFields(logrus.Fields{
		"workflow_id": workflowID,
		"node_id":     nodeID,
	})

	// Get user ID from auth middleware
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	// Parse workflow ID
	objID, err := primitive.ObjectIDFromHex(workflowID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workflow ID"})
		return
	}

	// Get workflow from database
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var workflow models.Workflow
	err = database.WorkflowColl.FindOne(ctx, bson.M{"_id": objID}).Decode(&workflow)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	// Find the node
	var node *models.WorkflowNode
	for i := range workflow.Nodes {
		if workflow.Nodes[i].ID == nodeID {
			node = &workflow.Nodes[i]
			break
		}
	}

	if node == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
		return
	}

	// Only support service nodes for now
	if node.Type != "service" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Diagnostics only supported for service nodes"})
		return
	}

	// Get Kubernetes client
	clientset, err := getKubernetesClient(ctx, workflow.ClusterID, userID)
	if err != nil {
		logger.WithError(err).Error("Failed to get Kubernetes client")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to cluster"})
		return
	}

	// Get service details from node data
	serviceName := getNodeDataString(node.Data, "name")
	if serviceName == "" {
		serviceName = nodeID
	}
	namespace := getNodeDataString(node.Data, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	// Run diagnostics
	diagnostics := services.NewServiceDiagnostics(clientset)
	result, err := diagnostics.DiagnoseService(ctx, namespace, serviceName)
	if err != nil {
		logger.WithError(err).Error("Failed to run diagnostics")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to run diagnostics"})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetFixTemplateHandler returns a fix template for a specific fix type
// GET /v1/api/workflows/:id/nodes/:nodeId/fix-template/:fixType
func GetFixTemplateHandler(c *gin.Context) {
	workflowID := c.Param("id")
	nodeID := c.Param("nodeId")
	fixType := c.Param("fixType")

	logger := logrus.WithFields(logrus.Fields{
		"workflow_id": workflowID,
		"node_id":     nodeID,
		"fix_type":    fixType,
	})

	// Get user ID from auth middleware
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	// Parse workflow ID
	objID, err := primitive.ObjectIDFromHex(workflowID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workflow ID"})
		return
	}

	// Get workflow from database
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var workflow models.Workflow
	err = database.WorkflowColl.FindOne(ctx, bson.M{"_id": objID}).Decode(&workflow)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	// Find the node
	var node *models.WorkflowNode
	for i := range workflow.Nodes {
		if workflow.Nodes[i].ID == nodeID {
			node = &workflow.Nodes[i]
			break
		}
	}

	if node == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
		return
	}

	// Get Kubernetes client
	clientset, err := getKubernetesClient(ctx, workflow.ClusterID, userID)
	if err != nil {
		logger.WithError(err).Error("Failed to get Kubernetes client")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to cluster"})
		return
	}

	// Build params from node data
	params := map[string]interface{}{
		"serviceName": getNodeDataString(node.Data, "name"),
		"namespace":   getNodeDataString(node.Data, "namespace"),
		"targetApp":   getNodeDataString(node.Data, "targetApp"),
		"servicePort": getNodeDataInt(node.Data, "port"),
		"targetPort":  getNodeDataInt(node.Data, "targetPort"),
	}

	// Set defaults
	if params["serviceName"] == "" {
		params["serviceName"] = nodeID
	}
	if params["namespace"] == "" {
		params["namespace"] = "default"
	}

	// Get fix template
	templateService := services.NewFixTemplateService(clientset)
	template, err := templateService.GetFixTemplate(ctx, fixType, params)
	if err != nil {
		logger.WithError(err).Error("Failed to get fix template")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, template)
}

// ApplyNodeFixRequest represents a fix application request
type ApplyNodeFixRequest struct {
	FixType string `json:"fixType" binding:"required"`
	YAML    string `json:"yaml" binding:"required"`
	Mode    string `json:"mode"` // "auto" or "manual"
}

// ApplyNodeFixHandler applies a fix to resolve node issues
// POST /v1/api/workflows/:id/nodes/:nodeId/fix
func ApplyNodeFixHandler(c *gin.Context) {
	workflowID := c.Param("id")
	nodeID := c.Param("nodeId")

	logger := logrus.WithFields(logrus.Fields{
		"workflow_id": workflowID,
		"node_id":     nodeID,
	})

	var req ApplyNodeFixRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Get user ID from auth middleware
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	// Parse workflow ID
	objID, err := primitive.ObjectIDFromHex(workflowID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workflow ID"})
		return
	}

	// Get workflow from database
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var workflow models.Workflow
	err = database.WorkflowColl.FindOne(ctx, bson.M{"_id": objID}).Decode(&workflow)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	// Get Kubernetes config
	config, err := getKubernetesConfig(ctx, workflow.ClusterID, userID)
	if err != nil {
		logger.WithError(err).Error("Failed to get Kubernetes config")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to cluster"})
		return
	}

	// Create manifest applier
	manifestApplier, err := applier.NewManifestApplier(config, "default")
	if err != nil {
		logger.WithError(err).Error("Failed to create manifest applier")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create applier"})
		return
	}

	// Apply the YAML
	logger.WithFields(logrus.Fields{
		"fix_type": req.FixType,
		"mode":     req.Mode,
	}).Info("Applying fix")

	result, err := manifestApplier.ApplyYAML(ctx, []byte(req.YAML))
	if err != nil {
		logger.WithError(err).Error("Failed to apply fix")

		// Provide detailed error message
		errorDetails := err.Error()
		if len(errorDetails) > maxErrorDetailsLength {
			errorDetails = errorDetails[:maxErrorDetailsLength] + "..."
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to apply Kubernetes manifest",
			"details": errorDetails,
			"fixType": req.FixType,
			"mode":    req.Mode,
		})
		return
	}

	// Build success message based on fix type
	successMessage := "Fix applied successfully"
	switch req.FixType {
	case "metallb-pool":
		successMessage = "MetalLB IP pool configured. LoadBalancer services will now receive external IPs."
		// Trigger controller restart for MetalLB fixes
		if err := restartMetalLBController(ctx, config); err != nil {
			logger.WithError(err).Warn("Failed to restart MetalLB controller")
			successMessage += " Note: Controller restart failed - you may need to restart MetalLB manually."
		} else {
			successMessage += " MetalLB controller restarted."
		}
	case "selector-fix":
		successMessage = "Service selector updated. Endpoints should now match pod labels."
	case "port-fix":
		successMessage = "Service ports updated. Connections should now route correctly."
	}

	logger.WithFields(logrus.Fields{
		"fix_type": req.FixType,
		"mode":     req.Mode,
		"result":   result,
	}).Info("Fix applied successfully")

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": successMessage,
		"result":  result,
		"fixType": req.FixType,
		"mode":    req.Mode,
	})
}

// Helper functions

func getKubernetesClient(ctx context.Context, clusterID string, userID primitive.ObjectID) (*k8s.Clientset, error) {
	clusterService := services.GetKubernetesClusterService()
	cluster, err := clusterService.GetClusterByName(ctx, userID, clusterID)
	if err != nil {
		return nil, err
	}

	return clusterService.CreateClusterConnection(cluster)
}

func getKubernetesConfig(ctx context.Context, clusterID string, userID primitive.ObjectID) (*rest.Config, error) {
	clusterService := services.GetKubernetesClusterService()
	cluster, err := clusterService.GetClusterByName(ctx, userID, clusterID)
	if err != nil {
		return nil, err
	}

	auth := clusterService.ClusterToAuthConfig(cluster)
	return auth.BuildRESTConfig()
}

func restartMetalLBController(ctx context.Context, config *rest.Config) error {
	clientset, err := k8s.NewForConfig(config)
	if err != nil {
		return err
	}

	// Patch deployment to trigger restart
	patchData := []byte(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"` + time.Now().Format(time.RFC3339) + `"}}}}}`)

	_, err = clientset.AppsV1().Deployments("metallb-system").Patch(
		ctx,
		"controller",
		ktypes.StrategicMergePatchType,
		patchData,
		metav1.PatchOptions{},
	)

	return err
}

func getNodeDataString(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	if val, ok := data[key].(string); ok {
		return val
	}
	return ""
}

func getNodeDataInt(data map[string]interface{}, key string) int {
	if data == nil {
		return 0
	}
	if val, ok := data[key].(float64); ok {
		return int(val)
	}
	if val, ok := data[key].(int); ok {
		return val
	}
	return 0
}
