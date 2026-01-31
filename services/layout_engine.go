package services

import (
	"github.com/KubeOrch/core/models"
	"github.com/sirupsen/logrus"
)

// LayoutEngine calculates node positions for the workflow canvas
type LayoutEngine struct {
	logger *logrus.Logger
}

// NewLayoutEngine creates a new layout engine
func NewLayoutEngine() *LayoutEngine {
	return &LayoutEngine{
		logger: logrus.New(),
	}
}

// Layout constants
const (
	// Horizontal spacing between layers
	LayerSpacingX = 300.0

	// Vertical spacing between nodes in same layer
	NodeSpacingY = 200.0

	// Starting positions
	StartX = 100.0
	StartY = 100.0

	// Layer X positions (traffic flows left to right: Ingress → Service → Deployment)
	LayerConfigMaps  = 0 // x = 100 (config inputs)
	LayerIngress     = 0 // x = 100 (entry point, at top)
	LayerServices    = 1 // x = 400 (traffic routing)
	LayerDeployments = 2 // x = 700 (workloads)
	LayerPVCs        = 3 // x = 1000 (storage)
)

// CalculateLayout positions nodes using a layered layout algorithm
func (e *LayoutEngine) CalculateLayout(nodes []models.WorkflowNode, edges []models.WorkflowEdge) map[string]models.NodePosition {
	positions := make(map[string]models.NodePosition)

	// Group nodes by type
	configMaps := e.filterByType(nodes, "configmap")
	secrets := e.filterByType(nodes, "secret")
	deployments := e.filterByType(nodes, "deployment")
	statefulsets := e.filterByType(nodes, "statefulset")
	services := e.filterByType(nodes, "service")
	ingresses := e.filterByType(nodes, "ingress")
	pvcs := e.filterByType(nodes, "persistentvolumeclaim")
	plugins := e.filterByType(nodes, "plugin")

	// Combine deployments and statefulsets
	workloads := append(deployments, statefulsets...)
	workloads = append(workloads, plugins...)

	// Combine configmaps and secrets
	configs := append(configMaps, secrets...)

	// Layer 0: ConfigMaps and Secrets (left side)
	y := StartY
	for _, node := range configs {
		positions[node.ID] = models.NodePosition{
			X: StartX,
			Y: y,
		}
		y += NodeSpacingY
	}

	// Layer 1: Services (center-left, routes traffic to deployments)
	y = StartY
	for _, node := range services {
		positions[node.ID] = models.NodePosition{
			X: StartX + LayerSpacingX,
			Y: y,
		}
		y += NodeSpacingY
	}

	// Layer 2: Deployments/StatefulSets (center-right)
	// Align deployments with their connected services if possible
	deploymentPositions := e.alignDeploymentsToServices(workloads, services, edges, positions)
	for nodeID, pos := range deploymentPositions {
		positions[nodeID] = pos
	}

	// If no alignment was possible, use default positioning
	y = StartY
	for _, node := range workloads {
		if _, exists := positions[node.ID]; !exists {
			positions[node.ID] = models.NodePosition{
				X: StartX + LayerSpacingX*2,
				Y: y,
			}
			y += NodeSpacingY
		}
	}

	// Ingress at leftmost position (entry point), at top
	y = StartY - NodeSpacingY // Place above services
	if y < 50 {
		y = 50
	}
	for i, node := range ingresses {
		positions[node.ID] = models.NodePosition{
			X: StartX,
			Y: y + float64(i)*NodeSpacingY,
		}
	}

	// Layer 4: PVCs (right side)
	y = StartY
	// Align PVCs with their connected deployments
	pvcPositions := e.alignPVCsToDeployments(pvcs, workloads, edges, positions)
	for nodeID, pos := range pvcPositions {
		positions[nodeID] = pos
	}

	// If no alignment was possible, use default positioning
	y = StartY
	for _, node := range pvcs {
		if _, exists := positions[node.ID]; !exists {
			positions[node.ID] = models.NodePosition{
				X: StartX + LayerSpacingX*3,
				Y: y,
			}
			y += NodeSpacingY
		}
	}

	return positions
}

// filterByType returns nodes of a specific type
func (e *LayoutEngine) filterByType(nodes []models.WorkflowNode, nodeType string) []models.WorkflowNode {
	result := make([]models.WorkflowNode, 0)
	for _, node := range nodes {
		if node.Type == nodeType {
			result = append(result, node)
		}
	}
	return result
}

// alignServicesToDeployments positions services next to their connected deployments
// Note: This function is kept for backward compatibility but the main layout now uses alignDeploymentsToServices
func (e *LayoutEngine) alignServicesToDeployments(services, deployments []models.WorkflowNode, edges []models.WorkflowEdge, existingPositions map[string]models.NodePosition) map[string]models.NodePosition {
	positions := make(map[string]models.NodePosition)

	// Build edge map: service -> deployment
	serviceToDeployment := make(map[string]string)
	for _, edge := range edges {
		// Check if source is a service and target is a deployment
		for _, svc := range services {
			if edge.Source == svc.ID {
				for _, dep := range deployments {
					if edge.Target == dep.ID {
						serviceToDeployment[svc.ID] = dep.ID
						break
					}
				}
			}
		}
	}

	// Position services aligned with their deployments
	for _, svc := range services {
		if depID, ok := serviceToDeployment[svc.ID]; ok {
			if depPos, exists := existingPositions[depID]; exists {
				positions[svc.ID] = models.NodePosition{
					X: StartX + LayerSpacingX,
					Y: depPos.Y,
				}
			}
		}
	}

	return positions
}

// alignDeploymentsToServices positions deployments next to their connected services
func (e *LayoutEngine) alignDeploymentsToServices(deployments, services []models.WorkflowNode, edges []models.WorkflowEdge, existingPositions map[string]models.NodePosition) map[string]models.NodePosition {
	positions := make(map[string]models.NodePosition)

	// Build edge map: deployment -> service (reverse lookup from service->deployment edges)
	deploymentToService := make(map[string]string)
	for _, edge := range edges {
		// Check if source is a service and target is a deployment
		for _, svc := range services {
			if edge.Source == svc.ID {
				for _, dep := range deployments {
					if edge.Target == dep.ID {
						deploymentToService[dep.ID] = svc.ID
						break
					}
				}
			}
		}
	}

	// Position deployments aligned with their services
	for _, dep := range deployments {
		if svcID, ok := deploymentToService[dep.ID]; ok {
			if svcPos, exists := existingPositions[svcID]; exists {
				positions[dep.ID] = models.NodePosition{
					X: StartX + LayerSpacingX*2,
					Y: svcPos.Y,
				}
			}
		}
	}

	return positions
}

// alignPVCsToDeployments positions PVCs next to their connected deployments
func (e *LayoutEngine) alignPVCsToDeployments(pvcs, deployments []models.WorkflowNode, edges []models.WorkflowEdge, existingPositions map[string]models.NodePosition) map[string]models.NodePosition {
	positions := make(map[string]models.NodePosition)

	// Build edge map: pvc -> deployment
	pvcToDeployment := make(map[string]string)
	for _, edge := range edges {
		for _, pvc := range pvcs {
			if edge.Source == pvc.ID {
				for _, dep := range deployments {
					if edge.Target == dep.ID {
						pvcToDeployment[pvc.ID] = dep.ID
						break
					}
				}
			}
		}
	}

	// Position PVCs aligned with their deployments
	for _, pvc := range pvcs {
		if depID, ok := pvcToDeployment[pvc.ID]; ok {
			if depPos, exists := existingPositions[depID]; exists {
				positions[pvc.ID] = models.NodePosition{
					X: StartX + LayerSpacingX*3,
					Y: depPos.Y,
				}
			}
		}
	}

	return positions
}

// ApplyPositions applies calculated positions to nodes
func (e *LayoutEngine) ApplyPositions(nodes []models.WorkflowNode, positions map[string]models.NodePosition) []models.WorkflowNode {
	result := make([]models.WorkflowNode, len(nodes))
	for i, node := range nodes {
		result[i] = node
		if pos, ok := positions[node.ID]; ok {
			result[i].Position = models.Position{
				X: pos.X,
				Y: pos.Y,
			}
		}
	}
	return result
}
