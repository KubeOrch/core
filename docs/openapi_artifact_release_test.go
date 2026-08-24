package docs_test

import (
	"os"
	"regexp"
	"strings"
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
		parameters, ok := operation["parameters"].([]any)
		require.True(t, ok, "%s post must declare parameters", path)
		assert.Contains(t, parameters, map[string]any{"$ref": "#/components/parameters/IdempotencyKey"})
		assert.NotEmpty(t, mustMap(t, operation["responses"])["413"])
		created := mustMap(t, mustMap(t, operation["responses"])["201"])
		headers := mustMap(t, created["headers"])
		assert.Equal(t, "#/components/headers/IdempotencyReplayed", mustMap(t, headers["Idempotency-Replayed"])["$ref"])
	}

	assert.Equal(t, false, mustMap(t, schemas["CreateArtifactRequest"])["additionalProperties"])
	assert.Equal(t, false, mustMap(t, schemas["CreateReleaseRequest"])["additionalProperties"])
	oneOf, ok := mustMap(t, schemas["CreateReleaseRequest"])["oneOf"].([]any)
	require.True(t, ok)
	require.Len(t, oneOf, 2)
	externalCI := mustMap(t, oneOf[0])
	assert.Equal(t, []any{"sourceReference"}, externalCI["required"])
	assert.Equal(t, []any{"external-ci"}, mustMap(t, mustMap(t, externalCI["properties"])["source"])["enum"])
	manual := mustMap(t, oneOf[1])
	assert.Equal(t, []any{"manual"}, mustMap(t, mustMap(t, manual["properties"])["source"])["enum"])
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

	pattern, ok := image["pattern"].(string)
	require.True(t, ok)
	matcher, err := regexp.Compile(pattern)
	require.NoError(t, err)
	assert.True(t, matcher.MatchString("ghcr.io/kubeorch/core@sha256:"+strings.Repeat("a", 64)))
	assert.False(t, matcher.MatchString("ghcr.io/kubeorch/core@sha256:"+strings.Repeat("A", 64)))
	assert.Equal(t, []any{"image", "source"}, mustMap(t, schemas["CreateArtifactRequest"])["required"])
}

func TestArtifactReleaseReferencesEncodeHTTPSCredentialSafety(t *testing.T) {
	data, err := os.ReadFile("openapi.yaml")
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal(data, &document))
	schemas := mustMap(t, mustMap(t, document["components"])["schemas"])
	safeReference := mustMap(t, schemas["SafeHTTPSReference"])
	pattern, ok := safeReference["pattern"].(string)
	require.True(t, ok)
	matcher, err := regexp.Compile(pattern)
	require.NoError(t, err)

	assert.True(t, matcher.MatchString("https://evidence.example/builds/123/sbom.json"))
	assert.False(t, matcher.MatchString("http://evidence.example/sbom.json"))
	assert.False(t, matcher.MatchString("https://user:password@evidence.example/sbom.json"))
	assert.False(t, matcher.MatchString("https://evidence.example/sbom.json?token=secret"))
	assert.False(t, matcher.MatchString("https://evidence.example/sbom.json#fragment"))
}
