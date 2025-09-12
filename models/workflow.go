package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// WorkflowStatus represents the status of a workflow
type WorkflowStatus string

const (
	WorkflowStatusDraft     WorkflowStatus = "draft"
	WorkflowStatusPublished WorkflowStatus = "published"
	WorkflowStatusArchived  WorkflowStatus = "archived"
)

// WorkflowNode represents a node in the workflow
type WorkflowNode struct {
	ID       string                 `json:"id" bson:"id"`
	Type     string                 `json:"type" bson:"type"` // deployment, condition, parallel, etc.
	Position Position               `json:"position" bson:"position"`
	Data     map[string]interface{} `json:"data" bson:"data"`
}

// Position represents x,y coordinates for a node
type Position struct {
	X float64 `json:"x" bson:"x"`
	Y float64 `json:"y" bson:"y"`
}

// WorkflowEdge represents a connection between nodes
type WorkflowEdge struct {
	ID     string `json:"id" bson:"id"`
	Source string `json:"source" bson:"source"` // source node ID
	Target string `json:"target" bson:"target"` // target node ID
	Type   string `json:"type" bson:"type"`     // edge type
}

// WorkflowVersion represents a version of the workflow
type WorkflowVersion struct {
	Version     int            `json:"version" bson:"version"`
	Nodes       []WorkflowNode `json:"nodes" bson:"nodes"`
	Edges       []WorkflowEdge `json:"edges" bson:"edges"`
	Description string         `json:"description" bson:"description"`
	CreatedAt   time.Time      `json:"created_at" bson:"created_at"`
	CreatedBy   string         `json:"created_by" bson:"created_by"`
}

// Workflow represents a complete workflow
type Workflow struct {
	ID          primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	Name        string             `json:"name" bson:"name"`
	Description string             `json:"description" bson:"description"`
	Status      WorkflowStatus     `json:"status" bson:"status"`
	Tags        []string           `json:"tags" bson:"tags"`
	
	// Current version data
	Nodes []WorkflowNode `json:"nodes" bson:"nodes"`
	Edges []WorkflowEdge `json:"edges" bson:"edges"`
	
	// Version history
	Versions      []WorkflowVersion `json:"versions" bson:"versions"`
	CurrentVersion int              `json:"current_version" bson:"current_version"`
	
	// Metadata
	OwnerID   primitive.ObjectID `json:"owner_id" bson:"owner_id"`
	ClusterID string             `json:"cluster_id" bson:"cluster_id"`
	TeamID    string             `json:"team_id" bson:"team_id,omitempty"`
	
	// Timestamps
	CreatedAt  time.Time  `json:"created_at" bson:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at" bson:"updated_at"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty" bson:"deleted_at,omitempty"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty" bson:"last_run_at,omitempty"`
	
	// Statistics
	RunCount      int `json:"run_count" bson:"run_count"`
	SuccessCount  int `json:"success_count" bson:"success_count"`
	FailureCount  int `json:"failure_count" bson:"failure_count"`
}

// WorkflowRun represents an execution of a workflow
type WorkflowRun struct {
	ID         primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	WorkflowID primitive.ObjectID `json:"workflow_id" bson:"workflow_id"`
	Version    int                `json:"version" bson:"version"`
	Status     string             `json:"status" bson:"status"` // running, completed, failed, cancelled
	
	// Execution details
	StartedAt   time.Time  `json:"started_at" bson:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty" bson:"completed_at,omitempty"`
	Duration    int64      `json:"duration" bson:"duration"` // in milliseconds
	
	// Node execution states
	NodeStates map[string]interface{} `json:"node_states" bson:"node_states"`
	
	// Results and logs
	Output map[string]interface{} `json:"output" bson:"output"`
	Logs   []string               `json:"logs" bson:"logs"`
	Error  string                 `json:"error,omitempty" bson:"error,omitempty"`
	
	// Trigger information
	TriggeredBy string                 `json:"triggered_by" bson:"triggered_by"` // manual, schedule, webhook
	TriggerData map[string]interface{} `json:"trigger_data" bson:"trigger_data"`
}