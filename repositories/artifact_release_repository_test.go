package repositories

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestArtifactAndReleaseCursorsAreBoundToKindWorkspaceAndApplication(t *testing.T) {
	workspaceID := primitive.NewObjectID()
	applicationID := primitive.NewObjectID()
	resourceID := primitive.NewObjectID()
	createdAt := time.Date(2026, time.August, 22, 10, 30, 0, 123000000, time.UTC)
	releaseQuery := releaseCursorQueryHash(applicationID)

	releaseCursor := encodeDomainCursor(releaseCursorKind, workspaceID, releaseQuery, createdAt, resourceID)
	decodedAt, decodedID, err := decodeDomainCursor(releaseCursor, releaseCursorKind, workspaceID, releaseQuery)
	require.NoError(t, err)
	assert.Equal(t, createdAt, decodedAt)
	assert.Equal(t, resourceID, decodedID)

	_, _, err = decodeDomainCursor(releaseCursor, releaseCursorKind, workspaceID, releaseCursorQueryHash(primitive.NewObjectID()))
	assert.ErrorIs(t, err, ErrInvalidCursor)
	_, _, err = decodeDomainCursor(releaseCursor, artifactCursorKind, workspaceID, releaseQuery)
	assert.ErrorIs(t, err, ErrInvalidCursor)
	_, _, err = decodeDomainCursor(releaseCursor, releaseCursorKind, primitive.NewObjectID(), releaseQuery)
	assert.ErrorIs(t, err, ErrInvalidCursor)

	artifactCursor := encodeDomainCursor(artifactCursorKind, workspaceID, [8]byte{}, createdAt, resourceID)
	_, _, err = decodeDomainCursor(artifactCursor, artifactCursorKind, workspaceID, [8]byte{})
	require.NoError(t, err)
	assert.Empty(t, encodeDomainCursor(artifactCursorKind, workspaceID, [8]byte{}, time.UnixMilli(-1), resourceID))
}
