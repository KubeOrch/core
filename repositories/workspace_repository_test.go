package repositories

import (
	"errors"
	"testing"

	"github.com/KubeOrch/core/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestWorkspaceCursorRoundTrip(t *testing.T) {
	id := primitive.NewObjectID()
	cursor := encodeWorkspaceCursor(id)

	decoded, err := decodeWorkspaceCursor(cursor)

	require.NoError(t, err)
	assert.Equal(t, id, decoded)
}

func TestWorkspaceCursorRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"", "not-base64!", "c2hvcnQ"} {
		_, err := decodeWorkspaceCursor(value)
		assert.ErrorIs(t, err, ErrInvalidCursor)
	}
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
