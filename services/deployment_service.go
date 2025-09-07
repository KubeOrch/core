package services

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/KubeOrch/core/models"
)

type DeploymentService struct {
}

func NewDeploymentService() *DeploymentService {
	return &DeploymentService{}
}

func (s *DeploymentService) ProcessDeployment(req *models.DeploymentRequest) (*models.DeploymentResponse, error) {
	log.Printf("[DeploymentService] Processing deployment request for ID: %s", req.ID)
	
	reqJSON, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		log.Printf("[DeploymentService] Error marshaling request: %v", err)
		return nil, fmt.Errorf("failed to process request: %v", err)
	}
	
	log.Printf("[DeploymentService] Received deployment configuration:\n%s", string(reqJSON))
	
	if err := s.validateDeployment(req); err != nil {
		log.Printf("[DeploymentService] Validation failed: %v", err)
		return nil, fmt.Errorf("validation failed: %v", err)
	}
	
	helmValues := s.convertToHelmValues(req)
	log.Printf("[DeploymentService] Converted to Helm values:\n%s", s.formatHelmValues(helmValues))
	
	resourceID := fmt.Sprintf("%s-%d", req.ID, time.Now().Unix())
	
	response := &models.DeploymentResponse{
		ID:         req.ID,
		Status:     "accepted",
		Message:    fmt.Sprintf("Deployment request for '%s' has been accepted and queued for processing", req.ID),
		ResourceID: resourceID,
		Timestamp:  time.Now().Unix(),
	}
	
	log.Printf("[DeploymentService] Deployment request processed successfully. Resource ID: %s", resourceID)
	
	return response, nil
}

func (s *DeploymentService) validateDeployment(req *models.DeploymentRequest) error {
	if req.Parameters.Replicas < 1 {
		return fmt.Errorf("replicas must be at least 1")
	}
	
	if req.Parameters.Port < 1 || req.Parameters.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	
	if req.Parameters.Image == "" {
		return fmt.Errorf("image cannot be empty")
	}
	
	log.Printf("[DeploymentService] Validation passed for deployment: %s", req.ID)
	return nil
}

func (s *DeploymentService) convertToHelmValues(req *models.DeploymentRequest) map[string]interface{} {
	values := map[string]interface{}{
		"name":       req.ID,
		"templateId": req.TemplateID,
		"image":      req.Parameters.Image,
		"replicas":   req.Parameters.Replicas,
		"port":       req.Parameters.Port,
	}
	
	if len(req.Parameters.Env) > 0 {
		values["env"] = req.Parameters.Env
	}
	
	if req.Parameters.Resources != nil {
		resources := map[string]interface{}{}
		if req.Parameters.Resources.Limits.CPU != "" || req.Parameters.Resources.Limits.Memory != "" {
			resources["limits"] = map[string]string{
				"cpu":    req.Parameters.Resources.Limits.CPU,
				"memory": req.Parameters.Resources.Limits.Memory,
			}
		}
		if req.Parameters.Resources.Requests.CPU != "" || req.Parameters.Resources.Requests.Memory != "" {
			resources["requests"] = map[string]string{
				"cpu":    req.Parameters.Resources.Requests.CPU,
				"memory": req.Parameters.Resources.Requests.Memory,
			}
		}
		if len(resources) > 0 {
			values["resources"] = resources
		}
	}
	
	if len(req.Parameters.Labels) > 0 {
		values["labels"] = req.Parameters.Labels
	}
	
	if len(req.Metadata) > 0 {
		values["metadata"] = req.Metadata
	}
	
	return values
}

func (s *DeploymentService) formatHelmValues(values map[string]interface{}) string {
	formatted, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error formatting values: %v", err)
	}
	return string(formatted)
}