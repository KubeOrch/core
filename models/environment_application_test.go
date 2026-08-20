package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplicationResponsePreservesDesiredStateAndHidesPersistenceFields(t *testing.T) {
	now := time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC)
	response := ApplicationResponse{
		ID:            "application-id",
		WorkspaceID:   "workspace-id",
		EnvironmentID: "environment-id",
		Name:          "checkout",
		DesiredState: map[string]any{
			"futureField": map[string]any{"enabled": true},
		},
		Status:    ApplicationStatusDraft,
		CreatedAt: now,
		UpdatedAt: now,
	}

	encoded, err := json.Marshal(response)

	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"futureField":{"enabled":true}`)
	assert.NotContains(t, string(encoded), "creation_key")
	assert.NotContains(t, string(encoded), "creation_hash")
	assert.NotContains(t, string(encoded), "created_by")
}

func TestApplicationRequestsRejectNullDesiredState(t *testing.T) {
	var createRequest CreateApplicationRequest
	err := json.Unmarshal([]byte(`{"environmentId":"environment-id","name":"checkout","desiredState":null}`), &createRequest)
	assert.EqualError(t, err, "desiredState must be a JSON object")

	var updateRequest UpdateApplicationRequest
	err = json.Unmarshal([]byte(`{"name":"checkout","desiredState":null}`), &updateRequest)
	assert.EqualError(t, err, "desiredState must be a JSON object")
}

func TestUpdateApplicationRequestTracksDesiredStatePresence(t *testing.T) {
	var absent UpdateApplicationRequest
	require.NoError(t, json.Unmarshal([]byte(`{"name":"checkout"}`), &absent))
	assert.False(t, absent.DesiredState.Set)

	var empty UpdateApplicationRequest
	require.NoError(t, json.Unmarshal([]byte(`{"desiredState":{}}`), &empty))
	assert.True(t, empty.DesiredState.Set)
	assert.NotNil(t, empty.DesiredState.Value)
	assert.Empty(t, empty.DesiredState.Value)
}
