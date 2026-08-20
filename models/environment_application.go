package models

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ApplicationStatus string

const (
	ApplicationStatusDraft    ApplicationStatus = "draft"
	ApplicationStatusArchived ApplicationStatus = "archived"
)

type DesiredState map[string]any

func (d *DesiredState) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("desiredState must be a JSON object")
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*d = value
	return nil
}

type OptionalDesiredState struct {
	Value map[string]any
	Set   bool
}

func (d *OptionalDesiredState) UnmarshalJSON(data []byte) error {
	var value DesiredState
	if err := value.UnmarshalJSON(data); err != nil {
		return err
	}
	d.Value = value
	d.Set = true
	return nil
}

type Environment struct {
	ID             primitive.ObjectID `bson:"_id" json:"-"`
	WorkspaceID    primitive.ObjectID `bson:"workspace_id" json:"-"`
	Name           string             `bson:"name" json:"-"`
	NormalizedName string             `bson:"normalized_name" json:"-"`
	Description    string             `bson:"description,omitempty" json:"-"`
	CreatedBy      primitive.ObjectID `bson:"created_by" json:"-"`
	CreationKey    string             `bson:"creation_key" json:"-"`
	CreationHash   string             `bson:"creation_hash" json:"-"`
	CreatedAt      time.Time          `bson:"created_at" json:"-"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"-"`
}

type Application struct {
	ID            primitive.ObjectID `bson:"_id" json:"-"`
	WorkspaceID   primitive.ObjectID `bson:"workspace_id" json:"-"`
	EnvironmentID primitive.ObjectID `bson:"environment_id" json:"-"`
	Name          string             `bson:"name" json:"-"`
	Description   string             `bson:"description,omitempty" json:"-"`
	DesiredState  map[string]any     `bson:"desired_state" json:"-"`
	CreatedBy     primitive.ObjectID `bson:"created_by" json:"-"`
	CreationKey   string             `bson:"creation_key" json:"-"`
	CreationHash  string             `bson:"creation_hash" json:"-"`
	ArchivedAt    *time.Time         `bson:"archived_at,omitempty" json:"-"`
	CreatedAt     time.Time          `bson:"created_at" json:"-"`
	UpdatedAt     time.Time          `bson:"updated_at" json:"-"`
}

type CreateEnvironmentRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type UpdateEnvironmentRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type CreateApplicationRequest struct {
	EnvironmentID string       `json:"environmentId"`
	Name          string       `json:"name"`
	Description   string       `json:"description,omitempty"`
	DesiredState  DesiredState `json:"desiredState,omitempty"`
}

type UpdateApplicationRequest struct {
	Name         *string              `json:"name,omitempty"`
	Description  *string              `json:"description,omitempty"`
	DesiredState OptionalDesiredState `json:"desiredState,omitempty"`
}

type EnvironmentResponse struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type ApplicationResponse struct {
	ID            string            `json:"id"`
	WorkspaceID   string            `json:"workspaceId"`
	EnvironmentID string            `json:"environmentId"`
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	DesiredState  map[string]any    `json:"desiredState"`
	Status        ApplicationStatus `json:"status"`
	ArchivedAt    *time.Time        `json:"archivedAt,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

type EnvironmentListResponse struct {
	Items    []EnvironmentResponse `json:"items"`
	PageInfo PageInfo              `json:"pageInfo"`
}

type ApplicationListResponse struct {
	Items    []ApplicationResponse `json:"items"`
	PageInfo PageInfo              `json:"pageInfo"`
}
