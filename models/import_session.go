package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ImportSessionStatus represents the status of an import session
type ImportSessionStatus string

const (
	ImportStatusPending   ImportSessionStatus = "pending"
	ImportStatusCloning   ImportSessionStatus = "cloning"
	ImportStatusAnalyzing ImportSessionStatus = "analyzing"
	ImportStatusCompleted ImportSessionStatus = "completed"
	ImportStatusFailed    ImportSessionStatus = "failed"
)

// ImportSession tracks an async import operation
type ImportSession struct {
	ID           primitive.ObjectID  `json:"id" bson:"_id,omitempty"`
	UserID       primitive.ObjectID  `json:"userId" bson:"user_id"`
	Source       ImportSource        `json:"source" bson:"source"`
	URL          string              `json:"url,omitempty" bson:"url,omitempty"`
	Branch       string              `json:"branch,omitempty" bson:"branch,omitempty"`
	FileName     string              `json:"fileName,omitempty" bson:"file_name,omitempty"`
	Namespace    string              `json:"namespace" bson:"namespace"`
	Status       ImportSessionStatus `json:"status" bson:"status"`
	CurrentStage string              `json:"currentStage" bson:"current_stage"`
	Progress     int                 `json:"progress" bson:"progress"`
	Analysis     *ImportAnalysis     `json:"analysis,omitempty" bson:"analysis,omitempty"`
	ErrorMessage string              `json:"errorMessage,omitempty" bson:"error_message,omitempty"`
	ErrorStage   string              `json:"errorStage,omitempty" bson:"error_stage,omitempty"`
	CreatedAt    time.Time           `json:"createdAt" bson:"created_at"`
	CompletedAt  *time.Time          `json:"completedAt,omitempty" bson:"completed_at,omitempty"`
}

// IsTerminal returns true if the session is in a terminal state
func (s *ImportSession) IsTerminal() bool {
	return s.Status == ImportStatusCompleted || s.Status == ImportStatusFailed
}

// GetImportStreamKey returns the SSE stream key for an import session
func GetImportStreamKey(sessionID primitive.ObjectID) string {
	return "import:" + sessionID.Hex()
}
