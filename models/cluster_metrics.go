package models

import "time"

// ComponentHealthStatus represents the health status of a Kubernetes component
type ComponentHealthStatus string

const (
	ComponentHealthy   ComponentHealthStatus = "healthy"
	ComponentUnhealthy ComponentHealthStatus = "unhealthy"
	ComponentUnknown   ComponentHealthStatus = "unknown"
)

// ComponentHealth represents the health status of a Kubernetes control plane component
type ComponentHealth struct {
	Name    string                `json:"name"`
	Status  ComponentHealthStatus `json:"status"`
	Message string                `json:"message,omitempty"`
}

// ResourceMetric represents usage metrics for a single resource type
type ResourceMetric struct {
	Used       int64   `json:"used"`       // in millicores for CPU, bytes for memory/storage
	Capacity   int64   `json:"capacity"`   // total capacity
	Percentage float64 `json:"percentage"` // usage percentage (0-100)
}

// ResourceUsage contains aggregated resource usage across the cluster
type ResourceUsage struct {
	CPU     ResourceMetric `json:"cpu"`
	Memory  ResourceMetric `json:"memory"`
	Storage ResourceMetric `json:"storage"`
}

// ClusterMetrics represents real-time metrics for a Kubernetes cluster
type ClusterMetrics struct {
	ClusterName string            `json:"clusterName"`
	Health      []ComponentHealth `json:"health"`
	Resources   ResourceUsage     `json:"resources"`
	NodeCount   int               `json:"nodeCount"`
	PodCount    int               `json:"podCount"`
	LastUpdated time.Time         `json:"lastUpdated"`
}
