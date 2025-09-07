package models

type DeploymentRequest struct {
	ID         string                 `json:"id" binding:"required"`
	TemplateID string                 `json:"templateId" binding:"required"`
	Parameters DeploymentParameters   `json:"parameters" binding:"required"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

type DeploymentParameters struct {
	Image     string                `json:"image" binding:"required"`
	Replicas  int                   `json:"replicas" binding:"min=1"`
	Port      int                   `json:"port" binding:"min=1,max=65535"`
	Env       map[string]string     `json:"env,omitempty"`
	Resources *ResourceRequirements `json:"resources,omitempty"`
	Labels    map[string]string     `json:"labels,omitempty"`
}

type ResourceRequirements struct {
	Limits   ResourceSpec `json:"limits,omitempty"`
	Requests ResourceSpec `json:"requests,omitempty"`
}

type ResourceSpec struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

type DeploymentResponse struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	ResourceID string `json:"resourceId,omitempty"`
	Timestamp  int64  `json:"timestamp"`
}
