package repositories

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
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

const (
	workspaceCursorVersion           byte = 1
	workspaceListCursorKind          byte = 1
	membershipListCursorKind         byte = 2
	workspaceCursorHeaderLength           = 14
	membershipCursorPayloadLength         = workspaceCursorHeaderLength + 12
	workspaceListCursorPayloadLength      = workspaceCursorHeaderLength + 8 + 12
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
	pipeline, err := buildWorkspaceListPipeline(userID, limit, cursor)
	if err != nil {
		return nil, "", err
	}
	cursorResult, err := r.collection.Aggregate(ctx, pipeline)
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
		last := workspaces[len(workspaces)-1]
		if len(last.Memberships) != 1 {
			return nil, "", errors.New("workspace list projection omitted caller membership")
		}
		nextCursor = encodeWorkspaceListCursor(userID, last.Memberships[0].CreatedAt, last.ID)
	}
	return workspaces, nextCursor, nil
}

func buildWorkspaceListPipeline(userID primitive.ObjectID, limit int, cursor string) (mongo.Pipeline, error) {
	activeMembership := bson.M{
		"user_id": userID,
		"status":  models.MembershipStatusActive,
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"memberships": bson.M{"$elemMatch": activeMembership}}}},
		{{Key: "$unwind", Value: "$memberships"}},
		{{Key: "$match", Value: bson.M{
			"memberships.user_id": userID,
			"memberships.status":  models.MembershipStatusActive,
		}}},
	}
	if cursor != "" {
		membershipCreatedAt, workspaceID, err := decodeWorkspaceListCursor(cursor, userID)
		if err != nil {
			return nil, err
		}
		pipeline = append(pipeline, bson.D{{Key: "$match", Value: bson.M{"$or": bson.A{
			bson.M{"memberships.created_at": bson.M{"$lt": membershipCreatedAt}},
			bson.M{"memberships.created_at": membershipCreatedAt, "_id": bson.M{"$lt": workspaceID}},
		}}}})
	}
	pipeline = append(pipeline,
		bson.D{{Key: "$sort", Value: bson.D{
			{Key: "memberships.created_at", Value: -1},
			{Key: "_id", Value: -1},
		}}},
		bson.D{{Key: "$limit", Value: int64(limit + 1)}},
		bson.D{{Key: "$project", Value: bson.M{
			"_id":         1,
			"name":        1,
			"description": 1,
			"created_at":  1,
			"updated_at":  1,
			"memberships": bson.A{"$memberships"},
		}}},
	)
	return pipeline, nil
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
	delta := ownerDelta(currentRole, newRole)
	if delta != 0 {
		update["$inc"] = bson.M{"owner_count": delta}
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
		cursorID, err := decodeMembershipCursor(cursor, workspaceID)
		if err != nil {
			return nil, "", err
		}
		memberships = membershipsAfterCursor(memberships, cursorID)
	}

	nextCursor := ""
	if len(memberships) > limit {
		memberships = memberships[:limit]
		nextCursor = encodeMembershipCursor(workspaceID, memberships[len(memberships)-1].ID)
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

func membershipsAfterCursor(memberships []models.Membership, cursorID primitive.ObjectID) []models.Membership {
	start := sort.Search(len(memberships), func(index int) bool {
		return bytes.Compare(memberships[index].ID[:], cursorID[:]) < 0
	})
	return memberships[start:]
}

func encodeWorkspaceListCursor(scopeID primitive.ObjectID, membershipCreatedAt time.Time, workspaceID primitive.ObjectID) string {
	payload := make([]byte, workspaceListCursorPayloadLength)
	writeCursorHeader(payload, workspaceListCursorKind, scopeID)
	binary.BigEndian.PutUint64(payload[workspaceCursorHeaderLength:workspaceCursorHeaderLength+8], uint64(membershipCreatedAt.UnixMilli()))
	copy(payload[workspaceCursorHeaderLength+8:], workspaceID[:])
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeWorkspaceListCursor(value string, scopeID primitive.ObjectID) (time.Time, primitive.ObjectID, error) {
	decoded, err := decodeCursor(value, workspaceListCursorPayloadLength, workspaceListCursorKind, scopeID)
	if err != nil {
		return time.Time{}, primitive.NilObjectID, err
	}
	membershipCreatedAt := time.UnixMilli(int64(binary.BigEndian.Uint64(decoded[workspaceCursorHeaderLength : workspaceCursorHeaderLength+8]))).UTC()
	var workspaceID primitive.ObjectID
	copy(workspaceID[:], decoded[workspaceCursorHeaderLength+8:])
	return membershipCreatedAt, workspaceID, nil
}

func encodeMembershipCursor(scopeID, membershipID primitive.ObjectID) string {
	payload := make([]byte, membershipCursorPayloadLength)
	writeCursorHeader(payload, membershipListCursorKind, scopeID)
	copy(payload[workspaceCursorHeaderLength:], membershipID[:])
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeMembershipCursor(value string, scopeID primitive.ObjectID) (primitive.ObjectID, error) {
	decoded, err := decodeCursor(value, membershipCursorPayloadLength, membershipListCursorKind, scopeID)
	if err != nil {
		return primitive.NilObjectID, err
	}
	var membershipID primitive.ObjectID
	copy(membershipID[:], decoded[workspaceCursorHeaderLength:])
	return membershipID, nil
}

func writeCursorHeader(payload []byte, kind byte, scopeID primitive.ObjectID) {
	payload[0] = workspaceCursorVersion
	payload[1] = kind
	copy(payload[2:workspaceCursorHeaderLength], scopeID[:])
}

func decodeCursor(value string, expectedLength int, kind byte, scopeID primitive.ObjectID) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != expectedLength {
		return nil, fmt.Errorf("%w: malformed value", ErrInvalidCursor)
	}
	if decoded[0] != workspaceCursorVersion || decoded[1] != kind || !bytes.Equal(decoded[2:workspaceCursorHeaderLength], scopeID[:]) {
		return nil, fmt.Errorf("%w: malformed value", ErrInvalidCursor)
	}
	return decoded, nil
}
