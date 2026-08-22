package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArtifactResponseDoesNotExposePersistenceFields(t *testing.T) {
	response := ArtifactResponse{
		ID: "artifact-id", WorkspaceID: "workspace-id", Image: "registry.example/app@sha256:digest",
		Digest: "sha256:digest", Source: ArtifactSource{Repository: "https://example.com/repo", Ref: "main", SHA: "sha"},
		Evidence: ArtifactEvidence{}, CreatedBy: "actor-id", CreatedAt: time.Now().UTC(),
	}

	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "identity_hash")
	assert.NotContains(t, string(encoded), "creation_key")
	assert.NotContains(t, string(encoded), "creation_hash")
	assert.Contains(t, string(encoded), `"evidence":{}`)
}

func TestReleaseSourceValidation(t *testing.T) {
	assert.True(t, ReleaseSourceExternalCI.IsValid())
	assert.True(t, ReleaseSourceManual.IsValid())
	assert.False(t, ReleaseSource("automation").IsValid())
}
