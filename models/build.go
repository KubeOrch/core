package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// BuildStatus represents the current state of a build
type BuildStatus string

const (
	BuildStatusPending   BuildStatus = "pending"
	BuildStatusCloning   BuildStatus = "cloning"
	BuildStatusBuilding  BuildStatus = "building"
	BuildStatusPushing   BuildStatus = "pushing"
	BuildStatusCompleted BuildStatus = "completed"
	BuildStatusFailed    BuildStatus = "failed"
	BuildStatusCancelled BuildStatus = "cancelled"
)

// Build represents a container image build job
type Build struct {
	ID         primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	WorkflowID *primitive.ObjectID `bson:"workflow_id,omitempty" json:"workflowId,omitempty"`
	UserID     primitive.ObjectID  `bson:"user_id" json:"userId"`

	// Source configuration
	RepoURL      string            `bson:"repo_url" json:"repoUrl"`
	Branch       string            `bson:"branch" json:"branch"`
	CommitSHA    string            `bson:"commit_sha,omitempty" json:"commitSha,omitempty"`
	BuildContext string            `bson:"build_context" json:"buildContext"` // Path within repo, default "."
	Dockerfile   string            `bson:"dockerfile,omitempty" json:"dockerfile,omitempty"`
	BuildArgs    map[string]string `bson:"build_args,omitempty" json:"buildArgs,omitempty"`
	UseNixpacks  bool              `bson:"use_nixpacks" json:"useNixpacks"`

	// Target registry
	RegistryID primitive.ObjectID `bson:"registry_id" json:"registryId"`
	ImageName  string             `bson:"image_name" json:"imageName"` // e.g., "ghcr.io/user/app"
	ImageTag   string             `bson:"image_tag" json:"imageTag"`   // e.g., "v1.0.0" or "latest"

	// Status tracking
	Status       BuildStatus `bson:"status" json:"status"`
	CurrentStage string      `bson:"current_stage" json:"currentStage"`
	Progress     int         `bson:"progress" json:"progress"` // 0-100

	// Results
	FinalImageRef string `bson:"final_image_ref,omitempty" json:"finalImageRef,omitempty"` // Full image reference with digest
	ImageDigest   string `bson:"image_digest,omitempty" json:"imageDigest,omitempty"`
	ImageSize     int64  `bson:"image_size,omitempty" json:"imageSize,omitempty"` // Size in bytes

	// Error handling
	ErrorMessage string `bson:"error_message,omitempty" json:"errorMessage,omitempty"`
	ErrorStage   string `bson:"error_stage,omitempty" json:"errorStage,omitempty"`

	// Timestamps
	CreatedAt   time.Time  `bson:"created_at" json:"createdAt"`
	StartedAt   *time.Time `bson:"started_at,omitempty" json:"startedAt,omitempty"`
	CompletedAt *time.Time `bson:"completed_at,omitempty" json:"completedAt,omitempty"`
	Duration    int64      `bson:"duration,omitempty" json:"duration,omitempty"` // Duration in milliseconds
}

// BuildLog represents a single log entry during build
type BuildLog struct {
	Timestamp time.Time `json:"timestamp"`
	Stage     string    `json:"stage"`
	Message   string    `json:"message"`
	Level     string    `json:"level"`            // info, warn, error
	Stream    string    `json:"stream,omitempty"` // stdout, stderr
}

// StartBuildRequest contains parameters for starting a build
type StartBuildRequest struct {
	RepoURL      string            `json:"repoUrl" binding:"required"`
	Branch       string            `json:"branch"`
	RegistryID   string            `json:"registryId" binding:"required"`
	ImageName    string            `json:"imageName" binding:"required"`
	ImageTag     string            `json:"imageTag"`
	WorkflowID   string            `json:"workflowId,omitempty"`
	BuildContext string            `json:"buildContext,omitempty"`
	Dockerfile   string            `json:"dockerfile,omitempty"`
	BuildArgs    map[string]string `json:"buildArgs,omitempty"`
	UseNixpacks  bool              `json:"useNixpacks"`
}

// IsTerminal returns true if the build is in a terminal state
func (b *Build) IsTerminal() bool {
	return b.Status == BuildStatusCompleted ||
		b.Status == BuildStatusFailed ||
		b.Status == BuildStatusCancelled
}

// IsInProgress returns true if the build is actively running
func (b *Build) IsInProgress() bool {
	return b.Status == BuildStatusCloning ||
		b.Status == BuildStatusBuilding ||
		b.Status == BuildStatusPushing
}
