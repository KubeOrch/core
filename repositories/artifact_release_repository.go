package repositories

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	ErrArtifactNotFound             = errors.New("artifact not found")
	ErrReleaseNotFound              = errors.New("release not found")
	ErrDuplicateArtifactCreationKey = errors.New("artifact creation key already exists")
	ErrDuplicateArtifactIdentity    = errors.New("artifact identity already exists")
	ErrDuplicateReleaseCreationKey  = errors.New("release creation key already exists")
)

const (
	artifactCursorKind byte = 3
	releaseCursorKind  byte = 4
)

type ArtifactReleaseRepository struct {
	artifacts    *mongo.Collection
	releases     *mongo.Collection
	applications *mongo.Collection
}

func NewArtifactReleaseRepository() *ArtifactReleaseRepository {
	return &ArtifactReleaseRepository{
		artifacts:    database.ArtifactColl,
		releases:     database.ReleaseColl,
		applications: database.ApplicationColl,
	}
}

func (r *ArtifactReleaseRepository) CreateArtifact(ctx context.Context, artifact *models.Artifact) error {
	_, err := r.artifacts.InsertOne(ctx, artifact)
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	if _, lookupErr := r.GetArtifactByCreationKey(ctx, artifact.WorkspaceID, artifact.CreatedBy, artifact.CreationKey); lookupErr == nil {
		return ErrDuplicateArtifactCreationKey
	} else if !errors.Is(lookupErr, ErrArtifactNotFound) {
		return lookupErr
	}
	if _, lookupErr := r.GetArtifactByIdentity(ctx, artifact.WorkspaceID, artifact.IdentityHash); lookupErr == nil {
		return ErrDuplicateArtifactIdentity
	} else if !errors.Is(lookupErr, ErrArtifactNotFound) {
		return lookupErr
	}
	return fmt.Errorf("artifact insert reported an unclassified duplicate key")
}

func (r *ArtifactReleaseRepository) GetArtifactByCreationKey(
	ctx context.Context,
	workspaceID, actorID primitive.ObjectID,
	key string,
) (*models.Artifact, error) {
	return r.findArtifact(ctx, bson.M{
		"workspace_id": workspaceID,
		"created_by":   actorID,
		"creation_key": key,
	})
}

func (r *ArtifactReleaseRepository) GetArtifactByIdentity(
	ctx context.Context,
	workspaceID primitive.ObjectID,
	identityHash string,
) (*models.Artifact, error) {
	return r.findArtifact(ctx, bson.M{"workspace_id": workspaceID, "identity_hash": identityHash})
}

func (r *ArtifactReleaseRepository) ListArtifacts(
	ctx context.Context,
	workspaceID primitive.ObjectID,
	limit int,
	cursor string,
) ([]models.Artifact, string, error) {
	filter := bson.M{"workspace_id": workspaceID}
	if cursor != "" {
		createdAt, artifactID, err := decodeDomainCursor(cursor, artifactCursorKind, workspaceID, [8]byte{})
		if err != nil {
			return nil, "", err
		}
		filter["$or"] = descendingCursorFilter(createdAt, artifactID)
	}

	result, err := r.artifacts.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetLimit(int64(limit+1)))
	if err != nil {
		return nil, "", err
	}
	defer closeMongoCursor(ctx, result, "artifact")

	var artifacts []models.Artifact
	if err := result.All(ctx, &artifacts); err != nil {
		return nil, "", err
	}
	nextCursor := ""
	if len(artifacts) > limit {
		artifacts = artifacts[:limit]
		last := artifacts[len(artifacts)-1]
		nextCursor = encodeDomainCursor(artifactCursorKind, workspaceID, [8]byte{}, last.CreatedAt, last.ID)
	}
	return artifacts, nextCursor, nil
}

func (r *ArtifactReleaseRepository) GetArtifact(
	ctx context.Context,
	workspaceID, artifactID primitive.ObjectID,
) (*models.Artifact, error) {
	return r.findArtifact(ctx, bson.M{"_id": artifactID, "workspace_id": workspaceID})
}

func (r *ArtifactReleaseRepository) ArtifactsExist(
	ctx context.Context,
	workspaceID primitive.ObjectID,
	artifactIDs []primitive.ObjectID,
) (bool, error) {
	count, err := r.artifacts.CountDocuments(ctx, bson.M{
		"workspace_id": workspaceID,
		"_id":          bson.M{"$in": artifactIDs},
	})
	return count == int64(len(artifactIDs)), err
}

func (r *ArtifactReleaseRepository) GetApplication(
	ctx context.Context,
	workspaceID, applicationID primitive.ObjectID,
) (*models.Application, error) {
	var application models.Application
	err := r.applications.FindOne(ctx, bson.M{"_id": applicationID, "workspace_id": workspaceID}).Decode(&application)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrApplicationNotFound
	}
	return &application, err
}

func (r *ArtifactReleaseRepository) CreateRelease(ctx context.Context, release *models.Release) error {
	_, err := r.releases.InsertOne(ctx, release)
	if mongo.IsDuplicateKeyError(err) {
		return ErrDuplicateReleaseCreationKey
	}
	return err
}

func (r *ArtifactReleaseRepository) GetReleaseByCreationKey(
	ctx context.Context,
	workspaceID, actorID primitive.ObjectID,
	key string,
) (*models.Release, error) {
	return r.findRelease(ctx, bson.M{
		"workspace_id": workspaceID,
		"created_by":   actorID,
		"creation_key": key,
	})
}

func (r *ArtifactReleaseRepository) ListReleases(
	ctx context.Context,
	workspaceID, applicationID primitive.ObjectID,
	limit int,
	cursor string,
) ([]models.Release, string, error) {
	filter := bson.M{"workspace_id": workspaceID, "application_id": applicationID}
	queryHash := releaseCursorQueryHash(applicationID)
	if cursor != "" {
		createdAt, releaseID, err := decodeDomainCursor(cursor, releaseCursorKind, workspaceID, queryHash)
		if err != nil {
			return nil, "", err
		}
		filter["$or"] = descendingCursorFilter(createdAt, releaseID)
	}

	result, err := r.releases.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetLimit(int64(limit+1)))
	if err != nil {
		return nil, "", err
	}
	defer closeMongoCursor(ctx, result, "release")

	var releases []models.Release
	if err := result.All(ctx, &releases); err != nil {
		return nil, "", err
	}
	nextCursor := ""
	if len(releases) > limit {
		releases = releases[:limit]
		last := releases[len(releases)-1]
		nextCursor = encodeDomainCursor(releaseCursorKind, workspaceID, queryHash, last.CreatedAt, last.ID)
	}
	return releases, nextCursor, nil
}

func (r *ArtifactReleaseRepository) GetRelease(
	ctx context.Context,
	workspaceID, applicationID, releaseID primitive.ObjectID,
) (*models.Release, error) {
	return r.findRelease(ctx, bson.M{
		"_id":            releaseID,
		"workspace_id":   workspaceID,
		"application_id": applicationID,
	})
}

func (r *ArtifactReleaseRepository) findArtifact(ctx context.Context, filter bson.M) (*models.Artifact, error) {
	var artifact models.Artifact
	err := r.artifacts.FindOne(ctx, filter).Decode(&artifact)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrArtifactNotFound
	}
	return &artifact, err
}

func (r *ArtifactReleaseRepository) findRelease(ctx context.Context, filter bson.M) (*models.Release, error) {
	var release models.Release
	err := r.releases.FindOne(ctx, filter).Decode(&release)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrReleaseNotFound
	}
	return &release, err
}

func releaseCursorQueryHash(applicationID primitive.ObjectID) [8]byte {
	hash := sha256.Sum256(applicationID[:])
	var result [8]byte
	copy(result[:], hash[:])
	return result
}
