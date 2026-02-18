package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type NotificationType string

const (
	NotificationWorkflowDeployed NotificationType = "workflow_deployed"
	NotificationWorkflowFailed   NotificationType = "workflow_failed"
	NotificationBuildCompleted   NotificationType = "build_completed"
	NotificationBuildFailed      NotificationType = "build_failed"
	NotificationResourceWarning  NotificationType = "resource_warning"
	NotificationSystem           NotificationType = "system"
)

type Notification struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    primitive.ObjectID `bson:"user_id" json:"userId"`
	Type      NotificationType   `bson:"type" json:"type"`
	Title     string             `bson:"title" json:"title"`
	Message   string             `bson:"message" json:"message"`
	Read      bool               `bson:"read" json:"read"`
	Link      string             `bson:"link,omitempty" json:"link,omitempty"`
	RefID     string             `bson:"ref_id,omitempty" json:"refId,omitempty"`
	CreatedAt time.Time          `bson:"created_at" json:"createdAt"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updatedAt"`
}
