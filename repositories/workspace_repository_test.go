package repositories

import (
	"errors"
	"testing"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestWorkspaceListCursorRoundTrip(t *testing.T) {
	scopeID := primitive.NewObjectID()
	membershipCreatedAt := time.Date(2026, time.August, 19, 12, 30, 0, 123000000, time.UTC)
	workspaceID := primitive.NewObjectID()
	cursor := encodeWorkspaceListCursor(scopeID, membershipCreatedAt, workspaceID)

	decodedCreatedAt, decodedWorkspaceID, err := decodeWorkspaceListCursor(cursor, scopeID)

	require.NoError(t, err)
	assert.Equal(t, membershipCreatedAt, decodedCreatedAt)
	assert.Equal(t, workspaceID, decodedWorkspaceID)
}

func TestWorkspaceCursorRejectsMalformedValues(t *testing.T) {
	scopeID := primitive.NewObjectID()
	for _, value := range []string{"", "not-base64!", "c2hvcnQ"} {
		_, _, err := decodeWorkspaceListCursor(value, scopeID)
		assert.ErrorIs(t, err, ErrInvalidCursor)
	}
}

func TestWorkspaceCursorRejectsWrongKindAndScope(t *testing.T) {
	scopeID := primitive.NewObjectID()
	cursor := encodeWorkspaceListCursor(scopeID, time.Now(), primitive.NewObjectID())

	_, err := decodeMembershipCursor(cursor, scopeID)
	assert.ErrorIs(t, err, ErrInvalidCursor)
	_, _, err = decodeWorkspaceListCursor(cursor, primitive.NewObjectID())
	assert.ErrorIs(t, err, ErrInvalidCursor)
}

func TestWorkspaceListPipelineOrdersByMembershipCreationTime(t *testing.T) {
	userID := primitive.NewObjectID()
	olderWorkspaceID := primitive.NewObjectIDFromTimestamp(time.Unix(100, 0))
	joinedAt := time.Unix(300, 0).UTC()
	cursor := encodeWorkspaceListCursor(userID, joinedAt, olderWorkspaceID)

	pipeline, err := buildWorkspaceListPipeline(userID, 20, cursor)

	require.NoError(t, err)
	var sortSpec bson.D
	for _, stage := range pipeline {
		if len(stage) == 1 && stage[0].Key == "$sort" {
			sortSpec = stage[0].Value.(bson.D)
			break
		}
	}
	assert.Equal(t, bson.D{
		{Key: "memberships.created_at", Value: -1},
		{Key: "_id", Value: -1},
	}, sortSpec)
	cursorMatch := pipeline[3][0].Value.(bson.M)
	assert.Equal(t, bson.A{
		bson.M{"memberships.created_at": bson.M{"$lt": joinedAt}},
		bson.M{"memberships.created_at": joinedAt, "_id": bson.M{"$lt": olderWorkspaceID}},
	}, cursorMatch["$or"])

	decodedJoinedAt, decodedWorkspaceID, err := decodeWorkspaceListCursor(cursor, userID)
	require.NoError(t, err)
	assert.Equal(t, joinedAt, decodedJoinedAt)
	assert.Equal(t, olderWorkspaceID, decodedWorkspaceID)
}

func TestMembershipCursorRoundTrip(t *testing.T) {
	workspaceID := primitive.NewObjectID()
	membershipID := primitive.NewObjectID()
	cursor := encodeMembershipCursor(workspaceID, membershipID)

	decoded, err := decodeMembershipCursor(cursor, workspaceID)

	require.NoError(t, err)
	assert.Equal(t, membershipID, decoded)
}

func TestMembershipCursorSurvivesRemovalOfCursorMember(t *testing.T) {
	newest := primitive.NewObjectIDFromTimestamp(time.Unix(300, 0))
	removedCursorMember := primitive.NewObjectIDFromTimestamp(time.Unix(200, 0))
	oldest := primitive.NewObjectIDFromTimestamp(time.Unix(100, 0))
	memberships := []models.Membership{{ID: newest}, {ID: oldest}}

	remaining := membershipsAfterCursor(memberships, removedCursorMember)

	require.Len(t, remaining, 1)
	assert.Equal(t, oldest, remaining[0].ID)
}

func TestOwnerDelta(t *testing.T) {
	tests := []struct {
		name    string
		current models.MembershipRole
		next    models.MembershipRole
		want    int
	}{
		{name: "promote owner", current: models.MembershipRoleAdmin, next: models.MembershipRoleOwner, want: 1},
		{name: "demote owner", current: models.MembershipRoleOwner, next: models.MembershipRoleMember, want: -1},
		{name: "non-owner change", current: models.MembershipRoleAdmin, next: models.MembershipRoleMember, want: 0},
		{name: "unchanged owner", current: models.MembershipRoleOwner, next: models.MembershipRoleOwner, want: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, ownerDelta(test.current, test.next))
		})
	}
}

func TestOwnerMutationFilterGuardsTheFinalOwnerInTheSameWrite(t *testing.T) {
	workspaceID := primitive.NewObjectID()
	ownerID := primitive.NewObjectID()
	membershipID := primitive.NewObjectID()

	filter := membershipMutationFilter(
		workspaceID,
		ownerID,
		membershipID,
		models.MembershipRoleOwner,
		[]models.MembershipRole{models.MembershipRoleOwner},
		true,
	)

	assert.Equal(t, workspaceID, filter["_id"])
	assert.Equal(t, bson.M{"$gt": 1}, filter["owner_count"])
	memberships := filter["memberships"].(bson.M)["$all"].(bson.A)
	require.Len(t, memberships, 2)
	actorMatch := memberships[0].(bson.M)["$elemMatch"].(bson.M)
	assert.Equal(t, ownerID, actorMatch["user_id"])
	assert.Equal(t, bson.M{"$in": []models.MembershipRole{models.MembershipRoleOwner}}, actorMatch["role"])
	targetMatch := memberships[1].(bson.M)["$elemMatch"].(bson.M)
	assert.Equal(t, membershipID, targetMatch["_id"])
	assert.Equal(t, models.MembershipRoleOwner, targetMatch["role"])
}

func TestNonOwnerMutationFilterDoesNotRequireAdditionalOwner(t *testing.T) {
	filter := membershipMutationFilter(
		primitive.NewObjectID(),
		primitive.NewObjectID(),
		primitive.NewObjectID(),
		models.MembershipRoleMember,
		[]models.MembershipRole{models.MembershipRoleOwner, models.MembershipRoleAdmin},
		false,
	)

	_, hasOwnerGuard := filter["owner_count"]
	assert.False(t, hasOwnerGuard)
}

func TestRepositoryErrorsRemainDistinct(t *testing.T) {
	assert.False(t, errors.Is(ErrWorkspaceNotFound, ErrMembershipNotFound))
	assert.False(t, errors.Is(ErrConcurrentUpdate, ErrMembershipExists))
}
