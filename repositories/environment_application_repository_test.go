package repositories

import (
	"encoding/base64"
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestDomainCursorRoundTripAndQueryBinding(t *testing.T) {
	workspaceID := primitive.NewObjectID()
	environmentID := primitive.NewObjectID()
	resourceID := primitive.NewObjectID()
	createdAt := time.Date(2026, time.August, 20, 9, 30, 0, 123000000, time.UTC)
	queryHash := applicationCursorQueryHash(&environmentID, true)
	cursor := encodeDomainCursor(applicationCursorKind, workspaceID, queryHash, createdAt, resourceID)

	decodedCreatedAt, decodedResourceID, err := decodeDomainCursor(cursor, applicationCursorKind, workspaceID, queryHash)

	require.NoError(t, err)
	assert.Equal(t, createdAt, decodedCreatedAt)
	assert.Equal(t, resourceID, decodedResourceID)
	_, _, err = decodeDomainCursor(cursor, applicationCursorKind, workspaceID, applicationCursorQueryHash(nil, true))
	assert.ErrorIs(t, err, ErrInvalidCursor)
	_, _, err = decodeDomainCursor(cursor, environmentCursorKind, workspaceID, queryHash)
	assert.ErrorIs(t, err, ErrInvalidCursor)
	_, _, err = decodeDomainCursor(cursor, applicationCursorKind, primitive.NewObjectID(), queryHash)
	assert.ErrorIs(t, err, ErrInvalidCursor)

	payload, err := base64.RawURLEncoding.DecodeString(cursor)
	require.NoError(t, err)
	binary.BigEndian.PutUint64(payload[domainCursorHeaderLength:domainCursorHeaderLength+8], ^uint64(0))
	forgedCursor := base64.RawURLEncoding.EncodeToString(payload)
	_, _, err = decodeDomainCursor(forgedCursor, applicationCursorKind, workspaceID, queryHash)
	assert.ErrorIs(t, err, ErrInvalidCursor)
}

func TestApplicationListFilterEnforcesWorkspaceEnvironmentAndArchiveState(t *testing.T) {
	workspaceID := primitive.NewObjectID()
	environmentID := primitive.NewObjectID()

	activeOnly := applicationListFilter(workspaceID, &environmentID, false)
	assert.Equal(t, workspaceID, activeOnly["workspace_id"])
	assert.Equal(t, environmentID, activeOnly["environment_id"])
	assert.Equal(t, bson.M{"$exists": false}, activeOnly["archived_at"])

	includingArchived := applicationListFilter(workspaceID, nil, true)
	assert.Equal(t, bson.M{"workspace_id": workspaceID}, includingArchived)
}

func TestDescendingCursorFilterUsesStableTimestampAndIDOrdering(t *testing.T) {
	createdAt := time.Date(2026, time.August, 20, 9, 0, 0, 0, time.UTC)
	resourceID := primitive.NewObjectID()

	assert.Equal(t, bson.A{
		bson.M{"created_at": bson.M{"$lt": createdAt}},
		bson.M{"created_at": createdAt, "_id": bson.M{"$lt": resourceID}},
	}, descendingCursorFilter(createdAt, resourceID))
}
