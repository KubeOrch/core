package repositories

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	ErrEnvironmentNotFound             = errors.New("environment not found")
	ErrApplicationNotFound             = errors.New("application not found")
	ErrEnvironmentNameExists           = errors.New("environment name already exists")
	ErrDuplicateEnvironmentCreationKey = errors.New("environment creation key already exists")
	ErrDuplicateApplicationCreationKey = errors.New("application creation key already exists")
)

const (
	domainCursorVersion       byte = 1
	environmentCursorKind     byte = 1
	applicationCursorKind     byte = 2
	domainCursorHeaderLength       = 22
	domainCursorPayloadLength      = domainCursorHeaderLength + 8 + 12
)

type EnvironmentApplicationRepository struct {
	environments *mongo.Collection
	applications *mongo.Collection
}

func NewEnvironmentApplicationRepository() *EnvironmentApplicationRepository {
	return &EnvironmentApplicationRepository{
		environments: database.EnvironmentColl,
		applications: database.ApplicationColl,
	}
}

func (r *EnvironmentApplicationRepository) CreateEnvironment(ctx context.Context, environment *models.Environment) error {
	_, err := r.environments.InsertOne(ctx, environment)
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	_, lookupErr := r.GetEnvironmentByCreationKey(ctx, environment.WorkspaceID, environment.CreatedBy, environment.CreationKey)
	if lookupErr == nil {
		return ErrDuplicateEnvironmentCreationKey
	}
	if !errors.Is(lookupErr, ErrEnvironmentNotFound) {
		return lookupErr
	}
	return ErrEnvironmentNameExists
}

func (r *EnvironmentApplicationRepository) GetEnvironmentByCreationKey(
	ctx context.Context,
	workspaceID, actorID primitive.ObjectID,
	key string,
) (*models.Environment, error) {
	var environment models.Environment
	err := r.environments.FindOne(ctx, bson.M{
		"workspace_id": workspaceID,
		"created_by":   actorID,
		"creation_key": key,
	}).Decode(&environment)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrEnvironmentNotFound
	}
	return &environment, err
}

func (r *EnvironmentApplicationRepository) ListEnvironments(
	ctx context.Context,
	workspaceID primitive.ObjectID,
	limit int,
	cursor string,
) ([]models.Environment, string, error) {
	filter := bson.M{"workspace_id": workspaceID}
	if cursor != "" {
		createdAt, environmentID, err := decodeDomainCursor(cursor, environmentCursorKind, workspaceID, [8]byte{})
		if err != nil {
			return nil, "", err
		}
		filter["$or"] = descendingCursorFilter(createdAt, environmentID)
	}

	result, err := r.environments.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetLimit(int64(limit+1)))
	if err != nil {
		return nil, "", err
	}
	defer closeMongoCursor(ctx, result, "environment")

	var environments []models.Environment
	if err := result.All(ctx, &environments); err != nil {
		return nil, "", err
	}
	nextCursor := ""
	if len(environments) > limit {
		environments = environments[:limit]
		last := environments[len(environments)-1]
		nextCursor = encodeDomainCursor(environmentCursorKind, workspaceID, [8]byte{}, last.CreatedAt, last.ID)
	}
	return environments, nextCursor, nil
}

func (r *EnvironmentApplicationRepository) GetEnvironment(
	ctx context.Context,
	workspaceID, environmentID primitive.ObjectID,
) (*models.Environment, error) {
	var environment models.Environment
	err := r.environments.FindOne(ctx, bson.M{"_id": environmentID, "workspace_id": workspaceID}).Decode(&environment)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrEnvironmentNotFound
	}
	return &environment, err
}

func (r *EnvironmentApplicationRepository) UpdateEnvironment(
	ctx context.Context,
	workspaceID, environmentID primitive.ObjectID,
	updates bson.M,
) (*models.Environment, error) {
	updates["updated_at"] = time.Now().UTC()
	var environment models.Environment
	err := r.environments.FindOneAndUpdate(
		ctx,
		bson.M{"_id": environmentID, "workspace_id": workspaceID},
		bson.M{"$set": updates},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&environment)
	if mongo.IsDuplicateKeyError(err) {
		return nil, ErrEnvironmentNameExists
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrEnvironmentNotFound
	}
	return &environment, err
}

func (r *EnvironmentApplicationRepository) CreateApplication(ctx context.Context, application *models.Application) error {
	_, err := r.applications.InsertOne(ctx, application)
	if mongo.IsDuplicateKeyError(err) {
		return ErrDuplicateApplicationCreationKey
	}
	return err
}

func (r *EnvironmentApplicationRepository) GetApplicationByCreationKey(
	ctx context.Context,
	workspaceID, actorID primitive.ObjectID,
	key string,
) (*models.Application, error) {
	var application models.Application
	err := r.applications.FindOne(ctx, bson.M{
		"workspace_id": workspaceID,
		"created_by":   actorID,
		"creation_key": key,
	}).Decode(&application)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrApplicationNotFound
	}
	return &application, err
}

func (r *EnvironmentApplicationRepository) ListApplications(
	ctx context.Context,
	workspaceID primitive.ObjectID,
	environmentID *primitive.ObjectID,
	includeArchived bool,
	limit int,
	cursor string,
) ([]models.Application, string, error) {
	filter := applicationListFilter(workspaceID, environmentID, includeArchived)
	queryHash := applicationCursorQueryHash(environmentID, includeArchived)
	if cursor != "" {
		createdAt, applicationID, err := decodeDomainCursor(cursor, applicationCursorKind, workspaceID, queryHash)
		if err != nil {
			return nil, "", err
		}
		filter["$or"] = descendingCursorFilter(createdAt, applicationID)
	}

	result, err := r.applications.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetLimit(int64(limit+1)))
	if err != nil {
		return nil, "", err
	}
	defer closeMongoCursor(ctx, result, "application")

	var applications []models.Application
	if err := result.All(ctx, &applications); err != nil {
		return nil, "", err
	}
	nextCursor := ""
	if len(applications) > limit {
		applications = applications[:limit]
		last := applications[len(applications)-1]
		nextCursor = encodeDomainCursor(applicationCursorKind, workspaceID, queryHash, last.CreatedAt, last.ID)
	}
	return applications, nextCursor, nil
}

func (r *EnvironmentApplicationRepository) GetApplication(
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

func (r *EnvironmentApplicationRepository) UpdateApplication(
	ctx context.Context,
	workspaceID, applicationID primitive.ObjectID,
	updates bson.M,
) (*models.Application, error) {
	updates["updated_at"] = time.Now().UTC()
	var application models.Application
	err := r.applications.FindOneAndUpdate(
		ctx,
		bson.M{"_id": applicationID, "workspace_id": workspaceID},
		bson.M{"$set": updates},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&application)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrApplicationNotFound
	}
	return &application, err
}

func (r *EnvironmentApplicationRepository) ArchiveApplication(
	ctx context.Context,
	workspaceID, applicationID primitive.ObjectID,
	archivedAt time.Time,
) (*models.Application, error) {
	var application models.Application
	err := r.applications.FindOneAndUpdate(
		ctx,
		bson.M{
			"_id":          applicationID,
			"workspace_id": workspaceID,
			"archived_at":  bson.M{"$exists": false},
		},
		bson.M{"$set": bson.M{"archived_at": archivedAt, "updated_at": archivedAt}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&application)
	if err == nil {
		return &application, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}
	return r.GetApplication(ctx, workspaceID, applicationID)
}

func applicationListFilter(
	workspaceID primitive.ObjectID,
	environmentID *primitive.ObjectID,
	includeArchived bool,
) bson.M {
	filter := bson.M{"workspace_id": workspaceID}
	if environmentID != nil {
		filter["environment_id"] = *environmentID
	}
	if !includeArchived {
		filter["archived_at"] = bson.M{"$exists": false}
	}
	return filter
}

func descendingCursorFilter(createdAt time.Time, resourceID primitive.ObjectID) bson.A {
	return bson.A{
		bson.M{"created_at": bson.M{"$lt": createdAt}},
		bson.M{"created_at": createdAt, "_id": bson.M{"$lt": resourceID}},
	}
}

func applicationCursorQueryHash(environmentID *primitive.ObjectID, includeArchived bool) [8]byte {
	hasher := sha256.New()
	if environmentID != nil {
		_, _ = hasher.Write(environmentID[:])
	}
	if includeArchived {
		_, _ = hasher.Write([]byte{1})
	} else {
		_, _ = hasher.Write([]byte{0})
	}
	var result [8]byte
	copy(result[:], hasher.Sum(nil))
	return result
}

func encodeDomainCursor(
	kind byte,
	workspaceID primitive.ObjectID,
	queryHash [8]byte,
	createdAt time.Time,
	resourceID primitive.ObjectID,
) string {
	timestampMillis := createdAt.UnixMilli()
	if timestampMillis < 0 {
		return ""
	}
	payload := make([]byte, domainCursorPayloadLength)
	payload[0] = domainCursorVersion
	payload[1] = kind
	copy(payload[2:14], workspaceID[:])
	copy(payload[14:domainCursorHeaderLength], queryHash[:])
	binary.BigEndian.PutUint64(payload[domainCursorHeaderLength:domainCursorHeaderLength+8], uint64(timestampMillis))
	copy(payload[domainCursorHeaderLength+8:], resourceID[:])
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeDomainCursor(
	value string,
	kind byte,
	workspaceID primitive.ObjectID,
	queryHash [8]byte,
) (time.Time, primitive.ObjectID, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(payload) != domainCursorPayloadLength {
		return time.Time{}, primitive.NilObjectID, fmt.Errorf("%w: malformed value", ErrInvalidCursor)
	}
	if payload[0] != domainCursorVersion || payload[1] != kind ||
		!bytes.Equal(payload[2:14], workspaceID[:]) ||
		!bytes.Equal(payload[14:domainCursorHeaderLength], queryHash[:]) {
		return time.Time{}, primitive.NilObjectID, fmt.Errorf("%w: malformed value", ErrInvalidCursor)
	}
	timestampMillis := binary.BigEndian.Uint64(payload[domainCursorHeaderLength : domainCursorHeaderLength+8])
	if timestampMillis > uint64(^uint64(0)>>1) {
		return time.Time{}, primitive.NilObjectID, fmt.Errorf("%w: malformed value", ErrInvalidCursor)
	}
	createdAt := time.UnixMilli(int64(timestampMillis)).UTC()
	var resourceID primitive.ObjectID
	copy(resourceID[:], payload[domainCursorHeaderLength+8:])
	return createdAt, resourceID, nil
}

func closeMongoCursor(ctx context.Context, cursor *mongo.Cursor, resource string) {
	if err := cursor.Close(ctx); err != nil {
		logrus.WithError(err).WithField("resource", resource).Warn("Failed to close list cursor")
	}
}
