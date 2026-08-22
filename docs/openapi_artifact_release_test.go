package docs_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestArtifactReleaseContractDeclaresOperationsAndBoundaries(t *testing.T) {
	data, err := os.ReadFile("openapi.yaml")
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal(data, &document))
	paths := mustMap(t, document["paths"])

	for _, expectation := range []struct {
		path, method, operationID, scope, success string
	}{
		{"/api/workspaces/{workspaceId}/artifacts", "get", "listArtifacts", "artifacts:read", "200"},
		{"/api/workspaces/{workspaceId}/artifacts", "post", "createArtifact", "artifacts:write", "201"},
		{"/api/workspaces/{workspaceId}/artifacts/{artifactId}", "get", "getArtifact", "artifacts:read", "200"},
		{"/api/workspaces/{workspaceId}/applications/{applicationId}/releases", "get", "listReleases", "releases:read", "200"},
		{"/api/workspaces/{workspaceId}/applications/{applicationId}/releases", "post", "createRelease", "releases:write", "201"},
		{"/api/workspaces/{workspaceId}/applications/{applicationId}/releases/{releaseId}", "get", "getRelease", "releases:read", "200"},
	} {
		t.Run(expectation.operationID, func(t *testing.T) {
			operation := mustMap(t, mustMap(t, paths[expectation.path])[expectation.method])
			assert.Equal(t, expectation.operationID, operation["operationId"])
			assert.Equal(t, "beta", operation["x-stability-level"])
			assert.Equal(t, "workspace", operation["x-kubeorch-workspace-boundary"])
			assert.Equal(t, []any{expectation.scope}, operation["x-kubeorch-required-scopes"])
			assert.Equal(t, []any{map[string]any{"bearerAuth": []any{}}}, operation["security"])
			success := mustMap(t, mustMap(t, operation["responses"])[expectation.success])
			headers := mustMap(t, success["headers"])
			assert.Equal(t, "#/components/headers/RequestId", mustMap(t, headers["X-Request-Id"])["$ref"])
		})
	}
}

func TestArtifactReleaseCreatesRequireIdempotencyAndRejectUnknownFields(t *testing.T) {
	data, err := os.ReadFile("openapi.yaml")
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal(data, &document))
	paths := mustMap(t, document["paths"])
	schemas := mustMap(t, mustMap(t, document["components"])["schemas"])

	for _, path := range []string{
		"/api/workspaces/{workspaceId}/artifacts",
		"/api/workspaces/{workspaceId}/applications/{applicationId}/releases",
	} {
		operation := mustMap(t, mustMap(t, paths[path])["post"])
		assert.Equal(t, "required", operation["x-kubeorch-idempotency"])
		parameters := operation["parameters"].([]any)
		assert.Contains(t, parameters, map[string]any{"$ref": "#/components/parameters/IdempotencyKey"})
		assert.NotEmpty(t, mustMap(t, operation["responses"])["413"])
		created := mustMap(t, mustMap(t, operation["responses"])["201"])
		headers := mustMap(t, created["headers"])
		assert.Equal(t, "#/components/headers/IdempotencyReplayed", mustMap(t, headers["Idempotency-Replayed"])["$ref"])
	}

	assert.Equal(t, false, mustMap(t, schemas["CreateArtifactRequest"])["additionalProperties"])
	assert.Equal(t, false, mustMap(t, schemas["CreateReleaseRequest"])["additionalProperties"])
	artifactIDs := mustMap(t, mustMap(t, schemas["CreateReleaseRequest"])["properties"])["artifactIds"]
	assert.Equal(t, true, mustMap(t, artifactIDs)["uniqueItems"])
}

func TestArtifactContractRequiresDigestPinnedImages(t *testing.T) {
	data, err := os.ReadFile("openapi.yaml")
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal(data, &document))
	schemas := mustMap(t, mustMap(t, document["components"])["schemas"])
	properties := mustMap(t, mustMap(t, schemas["CreateArtifactRequest"])["properties"])
	image := mustMap(t, properties["image"])

	assert.Contains(t, image["pattern"], "@sha256:")
	assert.Equal(t, []any{"image", "source"}, mustMap(t, schemas["CreateArtifactRequest"])["required"])
}
