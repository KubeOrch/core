package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceResponseDoesNotExposePersistenceFields(t *testing.T) {
	response := WorkspaceResponse{
		ID:        "workspace-id",
		Name:      "Platform",
		Role:      MembershipRoleOwner,
		CreatedAt: time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC),
	}

	encoded, err := json.Marshal(response)

	require.NoError(t, err)
	assert.JSONEq(t, `{
		"id":"workspace-id",
		"name":"Platform",
		"role":"owner",
		"createdAt":"2026-08-19T00:00:00Z",
		"updatedAt":"2026-08-19T00:00:00Z"
	}`, string(encoded))
	assert.NotContains(t, string(encoded), "owner_count")
	assert.NotContains(t, string(encoded), "creation_key")
	assert.NotContains(t, string(encoded), "memberships")
}

func TestMembershipRolesAreClosedSet(t *testing.T) {
	assert.True(t, MembershipRoleOwner.IsValid())
	assert.True(t, MembershipRoleAdmin.IsValid())
	assert.True(t, MembershipRoleMember.IsValid())
	assert.False(t, MembershipRole("custom").IsValid())
}
