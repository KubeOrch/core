package repositories

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
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
	ErrWorkspaceNotFound    = errors.New("workspace not found")
	ErrMembershipNotFound   = errors.New("membership not found")
	ErrMembershipExists     = errors.New("membership already exists")
	ErrDuplicateCreationKey = errors.New("workspace creation key already exists")
	ErrConcurrentUpdate     = errors.New("workspace changed concurrently")
	ErrInvalidCursor        = errors.New("invalid cursor")
)

type WorkspaceRepository struct {
	collection *mongo.Collection
}

func NewWorkspaceRepository() *WorkspaceRepository {
	return &WorkspaceRepository{collection: database.WorkspaceColl}
}

func (r *WorkspaceRepository) Create(ctx context.Context, workspace *models.Workspace) error {
	_, err := r.collection.InsertOne(ctx, workspace)
	if mongo.IsDuplicateKeyError(err) {
		return ErrDuplicateCreationKey
	}
	return err
}

func (r *WorkspaceRepository) GetByCreationKey(ctx context.Context, creatorID primitive.ObjectID, key string) (*models.Workspace, error) {
	var workspace models.Workspace
	err := r.collection.FindOne(ctx, bson.M{"created_by": creatorID, "creation_key": key}).Decode(&workspace)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrWorkspaceNotFound
	}
	return &workspace, err
}

func (r *WorkspaceRepository) GetForUser(ctx context.Context, workspaceID, userID primitive.ObjectID) (*models.Workspace, error) {
	filter := bson.M{
		"_id": workspaceID,
		"memberships": bson.M{"$elemMatch": bson.M{
			"user_id": userID,
			"status":  models.MembershipStatusActive,
		}},
	}

	var workspace models.Workspace
	err := r.collection.FindOne(ctx, filter).Decode(&workspace)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrWorkspaceNotFound
	}
	return &workspace, err
}

func (r *WorkspaceRepository) ListForUser(ctx context.Context, userID primitive.ObjectID, limit int, cursor string) ([]models.Workspace, string, error) {
	filter := bson.M{
		"memberships": bson.M{"$elemMatch": bson.M{
			"user_id": userID,
			"status":  models.MembershipStatusActive,
		}},
	}
	if cursor != "" {
		cursorID, err := decodeWorkspaceCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		filter["_id"] = bson.M{"$lt": cursorID}
	}

	findOptions := options.Find().
		SetSort(bson.D{{Key: "_id", Value: -1}}).
		SetLimit(int64(limit + 1)).
		SetProjection(bson.M{
			"name":        1,
			"description": 1,
			"created_at":  1,
			"updated_at":  1,
			"memberships": bson.M{"$elemMatch": bson.M{"user_id": userID, "status": models.MembershipStatusActive}},
		})

	cursorResult, err := r.collection.Find(ctx, filter, findOptions)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		if err := cursorResult.Close(ctx); err != nil {
			logrus.WithError(err).Warn("Failed to close workspace cursor")
		}
	}()

	var workspaces []models.Workspace
	if err := cursorResult.All(ctx, &workspaces); err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if len(workspaces) > limit {
		workspaces = workspaces[:limit]
		nextCursor = encodeWorkspaceCursor(workspaces[len(workspaces)-1].ID)
	}
	return workspaces, nextCursor, nil
}

func (r *WorkspaceRepository) UpdateMetadata(
	ctx context.Context,
	workspaceID, actorID primitive.ObjectID,
	updates bson.M,
) (*models.Workspace, error) {
	filter := authorizedWorkspaceFilter(workspaceID, actorID, []models.MembershipRole{
		models.MembershipRoleOwner,
		models.MembershipRoleAdmin,
	})
	updates["updated_at"] = time.Now().UTC()

	var workspace models.Workspace
	err := r.collection.FindOneAndUpdate(
		ctx,
		filter,
		bson.M{"$set": updates},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&workspace)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrWorkspaceNotFound
	}
	return &workspace, err
}

func (r *WorkspaceRepository) AddMembership(
	ctx context.Context,
	workspaceID, actorID primitive.ObjectID,
	membership models.Membership,
	ownerOnly bool,
) (*models.Workspace, error) {
	allowedRoles := []models.MembershipRole{models.MembershipRoleOwner, models.MembershipRoleAdmin}
	if ownerOnly {
		allowedRoles = []models.MembershipRole{models.MembershipRoleOwner}
	}
	filter := authorizedWorkspaceFilter(workspaceID, actorID, allowedRoles)
	filter["memberships.user_id"] = bson.M{"$ne": membership.UserID}

	update := bson.M{
		"$push": bson.M{"memberships": membership},
		"$set":  bson.M{"updated_at": time.Now().UTC()},
	}
	if membership.Role == models.MembershipRoleOwner {
		update["$inc"] = bson.M{"owner_count": 1}
	}

	var workspace models.Workspace
	err := r.collection.FindOneAndUpdate(
		ctx,
		filter,
		update,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&workspace)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrConcurrentUpdate
	}
	return &workspace, err
}

func (r *WorkspaceRepository) UpdateMembershipRole(
	ctx context.Context,
	workspaceID, actorID, membershipID primitive.ObjectID,
	currentRole, newRole models.MembershipRole,
	ownerOnly bool,
) (*models.Workspace, error) {
	allowedRoles := []models.MembershipRole{models.MembershipRoleOwner, models.MembershipRoleAdmin}
	if ownerOnly {
		allowedRoles = []models.MembershipRole{models.MembershipRoleOwner}
	}
	requiresAdditionalOwner := currentRole == models.MembershipRoleOwner && newRole != models.MembershipRoleOwner
	filter := membershipMutationFilter(workspaceID, actorID, membershipID, currentRole, allowedRoles, requiresAdditionalOwner)

	now := time.Now().UTC()
	update := bson.M{"$set": bson.M{
		"memberships.$[member].role":       newRole,
		"memberships.$[member].updated_at": now,
		"updated_at":                       now,
	}}
	ownerDelta := ownerDelta(currentRole, newRole)
	if ownerDelta != 0 {
		update["$inc"] = bson.M{"owner_count": ownerDelta}
	}

	var workspace models.Workspace
	err := r.collection.FindOneAndUpdate(
		ctx,
		filter,
		update,
		options.FindOneAndUpdate().
			SetArrayFilters(options.ArrayFilters{Filters: []interface{}{bson.M{"member._id": membershipID}}}).
			SetReturnDocument(options.After),
	).Decode(&workspace)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrConcurrentUpdate
	}
	return &workspace, err
}

func (r *WorkspaceRepository) RemoveMembership(
	ctx context.Context,
	workspaceID, actorID, membershipID primitive.ObjectID,
	currentRole models.MembershipRole,
	ownerOnly bool,
) error {
	allowedRoles := []models.MembershipRole{models.MembershipRoleOwner, models.MembershipRoleAdmin}
	if ownerOnly {
		allowedRoles = []models.MembershipRole{models.MembershipRoleOwner}
	}
	filter := membershipMutationFilter(
		workspaceID,
		actorID,
		membershipID,
		currentRole,
		allowedRoles,
		currentRole == models.MembershipRoleOwner,
	)

	update := bson.M{
		"$pull": bson.M{"memberships": bson.M{"_id": membershipID}},
		"$set":  bson.M{"updated_at": time.Now().UTC()},
	}
	if currentRole == models.MembershipRoleOwner {
		update["$inc"] = bson.M{"owner_count": -1}
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrConcurrentUpdate
	}
	return nil
}

func (r *WorkspaceRepository) ListMembershipsForUser(
	ctx context.Context,
	workspaceID, userID primitive.ObjectID,
	limit int,
	cursor string,
) ([]models.Membership, string, error) {
	workspace, err := r.GetForUser(ctx, workspaceID, userID)
	if err != nil {
		return nil, "", err
	}

	memberships := append([]models.Membership(nil), workspace.Memberships...)
	sort.Slice(memberships, func(i, j int) bool {
		return bytes.Compare(memberships[i].ID[:], memberships[j].ID[:]) > 0
	})

	if cursor != "" {
		cursorID, err := decodeWorkspaceCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		start := -1
		for index := range memberships {
			if memberships[index].ID == cursorID {
				start = index
				break
			}
		}
		if start < 0 {
			return nil, "", ErrInvalidCursor
		}
		memberships = memberships[start+1:]
	}

	nextCursor := ""
	if len(memberships) > limit {
		memberships = memberships[:limit]
		nextCursor = encodeWorkspaceCursor(memberships[len(memberships)-1].ID)
	}
	return memberships, nextCursor, nil
}

func authorizedWorkspaceFilter(workspaceID, actorID primitive.ObjectID, roles []models.MembershipRole) bson.M {
	return bson.M{
		"_id": workspaceID,
		"memberships": bson.M{"$elemMatch": bson.M{
			"user_id": actorID,
			"status":  models.MembershipStatusActive,
			"role":    bson.M{"$in": roles},
		}},
	}
}

func membershipMutationFilter(
	workspaceID, actorID, membershipID primitive.ObjectID,
	currentRole models.MembershipRole,
	allowedRoles []models.MembershipRole,
	requiresAdditionalOwner bool,
) bson.M {
	filter := bson.M{
		"_id": workspaceID,
		"memberships": bson.M{"$all": bson.A{
			bson.M{"$elemMatch": bson.M{
				"user_id": actorID,
				"status":  models.MembershipStatusActive,
				"role":    bson.M{"$in": allowedRoles},
			}},
			bson.M{"$elemMatch": bson.M{
				"_id":    membershipID,
				"status": models.MembershipStatusActive,
				"role":   currentRole,
			}},
		}},
	}
	if requiresAdditionalOwner {
		filter["owner_count"] = bson.M{"$gt": 1}
	}
	return filter
}

func ownerDelta(currentRole, newRole models.MembershipRole) int {
	if currentRole == models.MembershipRoleOwner && newRole != models.MembershipRoleOwner {
		return -1
	}
	if currentRole != models.MembershipRoleOwner && newRole == models.MembershipRoleOwner {
		return 1
	}
	return 0
}

func encodeWorkspaceCursor(id primitive.ObjectID) string {
	return base64.RawURLEncoding.EncodeToString(id[:])
}

func decodeWorkspaceCursor(value string) (primitive.ObjectID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != 12 {
		return primitive.NilObjectID, fmt.Errorf("%w: malformed value", ErrInvalidCursor)
	}
	var id primitive.ObjectID
	copy(id[:], decoded)
	return id, nil
}
