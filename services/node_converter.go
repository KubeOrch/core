package services

import (
	"fmt"
	"strings"

	"github.com/KubeOrch/core/models"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// NodeConverter converts imported services to workflow nodes
type NodeConverter struct {
	logger *logrus.Logger
}

// NewNodeConverter creates a new node converter
func NewNodeConverter() *NodeConverter {
	return &NodeConverter{
		logger: logrus.New(),
	}
}

// Stateful image patterns - these should use StatefulSet instead of Deployment
var statefulImagePatterns = []string{
	"postgres",
	"postgresql",
	"mysql",
	"mariadb",
	"mongodb",
	"mongo",
	"redis",
	"memcached",
	"elasticsearch",
	"cassandra",
	"couchdb",
	"couchbase",
	"neo4j",
	"influxdb",
	"timescaledb",
	"cockroachdb",
	"clickhouse",
	"kafka",
	"zookeeper",
	"rabbitmq",
	"nats",
	"etcd",
	"consul",
	"minio",
}

// ConversionResult contains the converted nodes and edges
type ConversionResult struct {
	Nodes    []models.WorkflowNode
	Edges    []models.WorkflowEdge
	Warnings []string
}

// Convert converts ImportedServices to WorkflowNodes and WorkflowEdges
func (c *NodeConverter) Convert(analysis *models.ImportAnalysis, namespace string) *ConversionResult {
	result := &ConversionResult{
		Nodes:    make([]models.WorkflowNode, 0),
		Edges:    make([]models.WorkflowEdge, 0),
		Warnings: make([]string, 0),
	}

	// Track node IDs for linking
	// serviceNameToDeploymentID maps service name to deployment node ID
	serviceNameToDeploymentID := make(map[string]string)
	// serviceNameToServiceID maps service name to service node ID (K8s Service)
	serviceNameToServiceID := make(map[string]string)
	// volumeNameToNodeID maps volume name to PVC node ID
	volumeNameToNodeID := make(map[string]string)
	// serviceNameToConfigMapID maps service name to ConfigMap node ID
	serviceNameToConfigMapID := make(map[string]string)
	// serviceNameToSecretID maps service name to Secret node ID
	serviceNameToSecretID := make(map[string]string)

	// First pass: Create PVC nodes for named volumes
	for _, vol := range analysis.Volumes {
		if !vol.External {
			pvcNode := c.createPVCNode(vol, namespace)
			result.Nodes = append(result.Nodes, pvcNode)
			volumeNameToNodeID[vol.Name] = pvcNode.ID
		}
	}

	// Second pass: Create nodes for each service
	for _, svc := range analysis.Services {
		// Separate sensitive vs non-sensitive environment variables
		sensitiveEnvs := make(map[string]string)
		regularEnvs := make(map[string]string)

		for k, v := range svc.Environment {
			if c.isSensitiveEnvVar(k) {
				sensitiveEnvs[k] = v
			} else {
				regularEnvs[k] = v
			}
		}

		// Create ConfigMap node if there are regular env vars
		var configMapNodeID string
		if len(regularEnvs) > 0 {
			configMapNode := c.createConfigMapNode(svc.Name, regularEnvs, namespace)
			result.Nodes = append(result.Nodes, configMapNode)
			configMapNodeID = configMapNode.ID
			serviceNameToConfigMapID[svc.Name] = configMapNodeID
		}

		// Create Secret node if there are sensitive env vars
		var secretNodeID string
		if len(sensitiveEnvs) > 0 {
			secretNode := c.createSecretNode(svc.Name, sensitiveEnvs, namespace)
			result.Nodes = append(result.Nodes, secretNode)
			secretNodeID = secretNode.ID
			serviceNameToSecretID[svc.Name] = secretNodeID
		}

		// Determine if this should be a StatefulSet (databases, caches, etc.) or Deployment
		var workloadNode models.WorkflowNode
		if c.isStatefulService(svc) {
			workloadNode = c.createStatefulSetNode(svc, namespace, configMapNodeID, secretNodeID, volumeNameToNodeID)
		} else {
			workloadNode = c.createDeploymentNode(svc, namespace, configMapNodeID, secretNodeID, volumeNameToNodeID)
		}
		result.Nodes = append(result.Nodes, workloadNode)
		serviceNameToDeploymentID[svc.Name] = workloadNode.ID

		// Create Service node if ports are exposed
		if len(svc.Ports) > 0 {
			serviceNode := c.createServiceNode(svc, namespace, workloadNode.ID)
			result.Nodes = append(result.Nodes, serviceNode)
			serviceNameToServiceID[svc.Name] = serviceNode.ID
		}
	}

	// Third pass: Create edges
	// Connect ConfigMaps to Deployments
	for svcName, configMapID := range serviceNameToConfigMapID {
		if deploymentID, ok := serviceNameToDeploymentID[svcName]; ok {
			result.Edges = append(result.Edges, models.WorkflowEdge{
				ID:     uuid.New().String(),
				Source: configMapID,
				Target: deploymentID,
			})
		}
	}

	// Connect Secrets to Deployments
	for svcName, secretID := range serviceNameToSecretID {
		if deploymentID, ok := serviceNameToDeploymentID[svcName]; ok {
			result.Edges = append(result.Edges, models.WorkflowEdge{
				ID:     uuid.New().String(),
				Source: secretID,
				Target: deploymentID,
			})
		}
	}

	// Connect PVCs to Deployments that use them
	for _, svc := range analysis.Services {
		deploymentID := serviceNameToDeploymentID[svc.Name]
		for _, vol := range svc.Volumes {
			if vol.Type == "volume" {
				if pvcID, ok := volumeNameToNodeID[vol.Source]; ok {
					result.Edges = append(result.Edges, models.WorkflowEdge{
						ID:     uuid.New().String(),
						Source: pvcID,
						Target: deploymentID,
					})
				}
			}
		}
	}

	// Connect Services to Deployments
	for svcName, serviceID := range serviceNameToServiceID {
		if deploymentID, ok := serviceNameToDeploymentID[svcName]; ok {
			result.Edges = append(result.Edges, models.WorkflowEdge{
				ID:     uuid.New().String(),
				Source: serviceID,
				Target: deploymentID,
			})
		}
	}

	return result
}

// createDeploymentNode creates a deployment node from an imported service
func (c *NodeConverter) createDeploymentNode(svc models.ImportedService, namespace, configMapID, secretID string, volumeNodeIDs map[string]string) models.WorkflowNode {
	nodeID := uuid.New().String()

	// Determine the primary port
	var port int
	if len(svc.Ports) > 0 {
		port = svc.Ports[0].ContainerPort
	}

	// Determine replicas
	replicas := svc.Replicas
	if replicas == 0 {
		replicas = 1
	}

	// Build volume mounts
	volumeMounts := make([]map[string]interface{}, 0)
	linkedPVCs := make([]string, 0)
	linkedConfigMaps := make([]string, 0)
	linkedSecrets := make([]string, 0)

	for _, vol := range svc.Volumes {
		if vol.Type == "volume" {
			if pvcNodeID, ok := volumeNodeIDs[vol.Source]; ok {
				volumeMounts = append(volumeMounts, map[string]interface{}{
					"type":      "persistentVolumeClaim",
					"name":      vol.Source,
					"mountPath": vol.Target,
					"nodeId":    pvcNodeID,
					"readOnly":  vol.ReadOnly,
				})
				linkedPVCs = append(linkedPVCs, pvcNodeID)
			}
		}
	}

	// Add ConfigMap mount if exists
	if configMapID != "" {
		linkedConfigMaps = append(linkedConfigMaps, configMapID)
	}

	// Add Secret mount if exists
	if secretID != "" {
		linkedSecrets = append(linkedSecrets, secretID)
	}

	// Build resource limits/requests
	resources := make(map[string]interface{})
	if svc.Resources != nil {
		if svc.Resources.Limits.Memory != "" || svc.Resources.Limits.CPUs != "" {
			limits := make(map[string]string)
			if svc.Resources.Limits.Memory != "" {
				limits["memory"] = svc.Resources.Limits.Memory
			}
			if svc.Resources.Limits.CPUs != "" {
				limits["cpu"] = svc.Resources.Limits.CPUs
			}
			resources["limits"] = limits
		}
		if svc.Resources.Reservations.Memory != "" || svc.Resources.Reservations.CPUs != "" {
			requests := make(map[string]string)
			if svc.Resources.Reservations.Memory != "" {
				requests["memory"] = svc.Resources.Reservations.Memory
			}
			if svc.Resources.Reservations.CPUs != "" {
				requests["cpu"] = svc.Resources.Reservations.CPUs
			}
			resources["requests"] = requests
		}
	}

	data := map[string]interface{}{
		"id":                nodeID,
		"name":              sanitizeK8sName(svc.Name),
		"namespace":         namespace,
		"image":             svc.Image,
		"replicas":          replicas,
		"port":              port,
		"templateId":        "deployment",
		"_linkedConfigMaps": linkedConfigMaps,
		"_linkedSecrets":    linkedSecrets,
		"_linkedPVCs":       linkedPVCs,
	}

	// Add volume mounts if any
	if len(volumeMounts) > 0 {
		data["volumeMounts"] = volumeMounts
	}

	// Add resources if any
	if len(resources) > 0 {
		data["resources"] = resources
	}

	// Add health check if configured
	if svc.HealthCheck != nil && len(svc.HealthCheck.Test) > 0 {
		// Convert docker health check to K8s probe format
		probeCmd := svc.HealthCheck.Test
		if len(probeCmd) > 0 && (probeCmd[0] == "CMD" || probeCmd[0] == "CMD-SHELL") {
			probeCmd = probeCmd[1:]
		}

		data["livenessProbe"] = map[string]interface{}{
			"exec": map[string]interface{}{
				"command": probeCmd,
			},
			"initialDelaySeconds": 30,
			"periodSeconds":       10,
		}
		data["readinessProbe"] = map[string]interface{}{
			"exec": map[string]interface{}{
				"command": probeCmd,
			},
			"initialDelaySeconds": 5,
			"periodSeconds":       5,
		}
	}

	// Add command if specified
	if len(svc.Command) > 0 {
		data["command"] = svc.Command
	}

	// Add args (entrypoint in docker-compose becomes command in K8s, command becomes args)
	if len(svc.Entrypoint) > 0 {
		data["command"] = svc.Entrypoint
		if len(svc.Command) > 0 {
			data["args"] = svc.Command
		}
	}

	return models.WorkflowNode{
		ID:       nodeID,
		Type:     "deployment",
		Position: models.Position{X: 0, Y: 0}, // Will be set by layout engine
		Data:     data,
	}
}

// createServiceNode creates a K8s Service node
func (c *NodeConverter) createServiceNode(svc models.ImportedService, namespace, deploymentNodeID string) models.WorkflowNode {
	nodeID := uuid.New().String()

	// Determine service type based on port mapping
	serviceType := "ClusterIP"
	var port, targetPort int

	if len(svc.Ports) > 0 {
		firstPort := svc.Ports[0]
		targetPort = firstPort.ContainerPort
		port = targetPort

		// If host port is specified, use NodePort
		if firstPort.HostPort > 0 {
			serviceType = "NodePort"
			port = firstPort.HostPort
		}
	}

	data := map[string]interface{}{
		"id":                 nodeID,
		"name":               sanitizeK8sName(svc.Name),
		"namespace":          namespace,
		"templateId":         "service",
		"serviceType":        serviceType,
		"port":               port,
		"targetPort":         targetPort,
		"targetApp":          sanitizeK8sName(svc.Name),
		"_linkedDeployment":  deploymentNodeID,
	}

	return models.WorkflowNode{
		ID:       nodeID,
		Type:     "service",
		Position: models.Position{X: 0, Y: 0},
		Data:     data,
	}
}

// createConfigMapNode creates a ConfigMap node for environment variables
func (c *NodeConverter) createConfigMapNode(serviceName string, envVars map[string]string, namespace string) models.WorkflowNode {
	nodeID := uuid.New().String()

	data := map[string]interface{}{
		"id":         nodeID,
		"name":       sanitizeK8sName(serviceName) + "-config",
		"namespace":  namespace,
		"templateId": "configmap",
		"data":       envVars,
		"mountPath":  "/etc/config",
	}

	return models.WorkflowNode{
		ID:       nodeID,
		Type:     "configmap",
		Position: models.Position{X: 0, Y: 0},
		Data:     data,
	}
}

// createSecretNode creates a Secret node for sensitive environment variables
func (c *NodeConverter) createSecretNode(serviceName string, envVars map[string]string, namespace string) models.WorkflowNode {
	nodeID := uuid.New().String()

	// For secrets, we store keys as objects with id and name (matching frontend SecretKeyEntry type)
	// Values are not stored - users must enter them in the UI
	keys := make([]map[string]string, 0, len(envVars))
	for k := range envVars {
		keys = append(keys, map[string]string{
			"id":   uuid.New().String(),
			"name": k,
		})
	}

	data := map[string]interface{}{
		"id":         nodeID,
		"name":       sanitizeK8sName(serviceName) + "-secrets",
		"namespace":  namespace,
		"templateId": "secret",
		"secretType": "Opaque",
		"keys":       keys,
		"mountPath":  "/etc/secrets",
	}

	return models.WorkflowNode{
		ID:       nodeID,
		Type:     "secret",
		Position: models.Position{X: 0, Y: 0},
		Data:     data,
	}
}

// createPVCNode creates a PersistentVolumeClaim node
func (c *NodeConverter) createPVCNode(vol models.ImportedVolume, namespace string) models.WorkflowNode {
	nodeID := uuid.New().String()

	data := map[string]interface{}{
		"id":               nodeID,
		"name":             sanitizeK8sName(vol.Name),
		"namespace":        namespace,
		"templateId":       "persistentvolumeclaim",
		"storage":          "10Gi", // Default storage size
		"accessModes":      []string{"ReadWriteOnce"},
		"storageClassName": "", // Use cluster default
		"volumeMode":       "Filesystem",
	}

	return models.WorkflowNode{
		ID:       nodeID,
		Type:     "persistentvolumeclaim",
		Position: models.Position{X: 0, Y: 0},
		Data:     data,
	}
}

// isSensitiveEnvVar checks if an environment variable name indicates sensitive data
func (c *NodeConverter) isSensitiveEnvVar(name string) bool {
	upperName := strings.ToUpper(name)
	for _, pattern := range models.SensitiveEnvPatterns {
		if strings.Contains(upperName, pattern) {
			return true
		}
	}
	return false
}

// sanitizeK8sName converts a name to be K8s compatible
// K8s names must be lowercase, alphanumeric, and can contain '-'
func sanitizeK8sName(name string) string {
	// Convert to lowercase
	result := strings.ToLower(name)

	// Replace underscores with hyphens
	result = strings.ReplaceAll(result, "_", "-")

	// Remove any characters that aren't alphanumeric or hyphens
	var sanitized strings.Builder
	for _, ch := range result {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			sanitized.WriteRune(ch)
		}
	}
	result = sanitized.String()

	// Remove leading/trailing hyphens
	result = strings.Trim(result, "-")

	// Ensure it doesn't start with a number
	if len(result) > 0 && result[0] >= '0' && result[0] <= '9' {
		result = "svc-" + result
	}

	// Truncate to 63 characters (K8s name limit)
	if len(result) > 63 {
		result = result[:63]
	}

	// Ensure not empty
	if result == "" {
		result = fmt.Sprintf("service-%s", uuid.New().String()[:8])
	}

	return result
}

// isStatefulService checks if a service should use StatefulSet based on its image
func (c *NodeConverter) isStatefulService(svc models.ImportedService) bool {
	// Check if image matches known stateful patterns
	imageLower := strings.ToLower(svc.Image)
	for _, pattern := range statefulImagePatterns {
		if strings.Contains(imageLower, pattern) {
			return true
		}
	}

	// Also check if the service has persistent volumes - a strong indicator of statefulness
	for _, vol := range svc.Volumes {
		if vol.Type == "volume" {
			// Named volume suggests persistent data
			return true
		}
	}

	return false
}

// createStatefulSetNode creates a StatefulSet node for stateful services (databases, caches, etc.)
func (c *NodeConverter) createStatefulSetNode(svc models.ImportedService, namespace, configMapID, secretID string, volumeNodeIDs map[string]string) models.WorkflowNode {
	nodeID := uuid.New().String()

	// Determine the primary port
	var port int
	if len(svc.Ports) > 0 {
		port = svc.Ports[0].ContainerPort
	}

	// Determine replicas
	replicas := svc.Replicas
	if replicas == 0 {
		replicas = 1
	}

	// Build volume mounts
	volumeMounts := make([]map[string]interface{}, 0)
	linkedPVCs := make([]string, 0)
	linkedConfigMaps := make([]string, 0)
	linkedSecrets := make([]string, 0)

	for _, vol := range svc.Volumes {
		if vol.Type == "volume" {
			if pvcNodeID, ok := volumeNodeIDs[vol.Source]; ok {
				volumeMounts = append(volumeMounts, map[string]interface{}{
					"type":      "persistentVolumeClaim",
					"name":      vol.Source,
					"mountPath": vol.Target,
					"nodeId":    pvcNodeID,
					"readOnly":  vol.ReadOnly,
				})
				linkedPVCs = append(linkedPVCs, pvcNodeID)
			}
		}
	}

	// Add ConfigMap mount if exists
	if configMapID != "" {
		linkedConfigMaps = append(linkedConfigMaps, configMapID)
	}

	// Add Secret mount if exists
	if secretID != "" {
		linkedSecrets = append(linkedSecrets, secretID)
	}

	// Build resource limits/requests
	resources := make(map[string]interface{})
	if svc.Resources != nil {
		if svc.Resources.Limits.Memory != "" || svc.Resources.Limits.CPUs != "" {
			limits := make(map[string]string)
			if svc.Resources.Limits.Memory != "" {
				limits["memory"] = svc.Resources.Limits.Memory
			}
			if svc.Resources.Limits.CPUs != "" {
				limits["cpu"] = svc.Resources.Limits.CPUs
			}
			resources["limits"] = limits
		}
		if svc.Resources.Reservations.Memory != "" || svc.Resources.Reservations.CPUs != "" {
			requests := make(map[string]string)
			if svc.Resources.Reservations.Memory != "" {
				requests["memory"] = svc.Resources.Reservations.Memory
			}
			if svc.Resources.Reservations.CPUs != "" {
				requests["cpu"] = svc.Resources.Reservations.CPUs
			}
			resources["requests"] = requests
		}
	}

	data := map[string]interface{}{
		"id":                nodeID,
		"name":              sanitizeK8sName(svc.Name),
		"namespace":         namespace,
		"image":             svc.Image,
		"replicas":          replicas,
		"port":              port,
		"templateId":        "statefulset",
		"serviceName":       sanitizeK8sName(svc.Name), // StatefulSet requires a headless service name
		"_linkedConfigMaps": linkedConfigMaps,
		"_linkedSecrets":    linkedSecrets,
		"_linkedPVCs":       linkedPVCs,
	}

	// Add volume mounts if any
	if len(volumeMounts) > 0 {
		data["volumeMounts"] = volumeMounts
	}

	// Add resources if any
	if len(resources) > 0 {
		data["resources"] = resources
	}

	// Add health check if configured
	if svc.HealthCheck != nil && len(svc.HealthCheck.Test) > 0 {
		probeCmd := svc.HealthCheck.Test
		if len(probeCmd) > 0 && (probeCmd[0] == "CMD" || probeCmd[0] == "CMD-SHELL") {
			probeCmd = probeCmd[1:]
		}

		data["livenessProbe"] = map[string]interface{}{
			"exec": map[string]interface{}{
				"command": probeCmd,
			},
			"initialDelaySeconds": 30,
			"periodSeconds":       10,
		}
		data["readinessProbe"] = map[string]interface{}{
			"exec": map[string]interface{}{
				"command": probeCmd,
			},
			"initialDelaySeconds": 5,
			"periodSeconds":       5,
		}
	}

	// Add command if specified
	if len(svc.Command) > 0 {
		data["command"] = svc.Command
	}

	// Add args
	if len(svc.Entrypoint) > 0 {
		data["command"] = svc.Entrypoint
		if len(svc.Command) > 0 {
			data["args"] = svc.Command
		}
	}

	return models.WorkflowNode{
		ID:       nodeID,
		Type:     "statefulset",
		Position: models.Position{X: 0, Y: 0},
		Data:     data,
	}
}
