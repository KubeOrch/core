package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ReleaseSource string

const (
	ReleaseSourceExternalCI ReleaseSource = "external-ci"
	ReleaseSourceManual     ReleaseSource = "manual"
)

type ArtifactSource struct {
	Repository string `bson:"repository" json:"repository"`
	Ref        string `bson:"ref" json:"ref"`
	SHA        string `bson:"sha" json:"sha"`
}

type ArtifactEvidence struct {
	SBOM       string `bson:"sbom,omitempty" json:"sbom,omitempty"`
	Provenance string `bson:"provenance,omitempty" json:"provenance,omitempty"`
	Scan       string `bson:"scan,omitempty" json:"scan,omitempty"`
	CIRun      string `bson:"ci_run,omitempty" json:"ciRun,omitempty"`
}

type Artifact struct {
	ID           primitive.ObjectID `bson:"_id" json:"-"`
	WorkspaceID  primitive.ObjectID `bson:"workspace_id" json:"-"`
	Image        string             `bson:"image" json:"-"`
	Digest       string             `bson:"digest" json:"-"`
	Source       ArtifactSource     `bson:"source" json:"-"`
	Evidence     ArtifactEvidence   `bson:"evidence" json:"-"`
	IdentityHash string             `bson:"identity_hash" json:"-"`
	CreatedBy    primitive.ObjectID `bson:"created_by" json:"-"`
	CreationKey  string             `bson:"creation_key" json:"-"`
	CreationHash string             `bson:"creation_hash" json:"-"`
	CreatedAt    time.Time          `bson:"created_at" json:"-"`
}

type Release struct {
	ID                  primitive.ObjectID   `bson:"_id" json:"-"`
	WorkspaceID         primitive.ObjectID   `bson:"workspace_id" json:"-"`
	ApplicationID       primitive.ObjectID   `bson:"application_id" json:"-"`
	ApplicationRevision string               `bson:"application_revision" json:"-"`
	ArtifactIDs         []primitive.ObjectID `bson:"artifact_ids" json:"-"`
	Source              ReleaseSource        `bson:"source" json:"-"`
	SourceReference     string               `bson:"source_reference,omitempty" json:"-"`
	CreatedBy           primitive.ObjectID   `bson:"created_by" json:"-"`
	CreationKey         string               `bson:"creation_key" json:"-"`
	CreationHash        string               `bson:"creation_hash" json:"-"`
	CreatedAt           time.Time            `bson:"created_at" json:"-"`
}

type CreateArtifactRequest struct {
	Image    string           `json:"image"`
	Source   ArtifactSource   `json:"source"`
	Evidence ArtifactEvidence `json:"evidence,omitempty"`
}

type CreateReleaseRequest struct {
	ApplicationRevision string        `json:"applicationRevision"`
	ArtifactIDs         []string      `json:"artifactIds"`
	Source              ReleaseSource `json:"source"`
	SourceReference     string        `json:"sourceReference,omitempty"`
}

type ArtifactResponse struct {
	ID          string           `json:"id"`
	WorkspaceID string           `json:"workspaceId"`
	Image       string           `json:"image"`
	Digest      string           `json:"digest"`
	Source      ArtifactSource   `json:"source"`
	Evidence    ArtifactEvidence `json:"evidence"`
	CreatedBy   string           `json:"createdBy"`
	CreatedAt   time.Time        `json:"createdAt"`
}

type ReleaseResponse struct {
	ID                  string        `json:"id"`
	WorkspaceID         string        `json:"workspaceId"`
	ApplicationID       string        `json:"applicationId"`
	ApplicationRevision string        `json:"applicationRevision"`
	ArtifactIDs         []string      `json:"artifactIds"`
	Source              ReleaseSource `json:"source"`
	SourceReference     string        `json:"sourceReference,omitempty"`
	CreatedBy           string        `json:"createdBy"`
	CreatedAt           time.Time     `json:"createdAt"`
}

type ArtifactListResponse struct {
	Items    []ArtifactResponse `json:"items"`
	PageInfo PageInfo           `json:"pageInfo"`
}

type ReleaseListResponse struct {
	Items    []ReleaseResponse `json:"items"`
	PageInfo PageInfo          `json:"pageInfo"`
}

func (s ReleaseSource) IsValid() bool {
	return s == ReleaseSourceExternalCI || s == ReleaseSourceManual
}
