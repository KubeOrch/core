package docs_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestWorkspaceContractDeclaresAllOperationsAndBoundaries(t *testing.T) {
	data, err := os.ReadFile("openapi.yaml")
	require.NoError(t, err)

	var document map[string]any
	require.NoError(t, yaml.Unmarshal(data, &document))
	paths := mustMap(t, document["paths"])

	expectations := []struct {
		path        string
		method      string
		operationID string
		boundary    string
		scope       string
	}{
		{path: "/api/workspaces", method: "get", operationID: "listWorkspaces", boundary: "identity", scope: "workspaces:read"},
		{path: "/api/workspaces", method: "post", operationID: "createWorkspace", boundary: "identity", scope: "workspaces:write"},
		{path: "/api/workspaces/{workspaceId}", method: "get", operationID: "getWorkspace", boundary: "workspace", scope: "workspaces:read"},
		{path: "/api/workspaces/{workspaceId}", method: "patch", operationID: "updateWorkspace", boundary: "workspace", scope: "workspaces:write"},
		{path: "/api/workspaces/{workspaceId}/members", method: "get", operationID: "listWorkspaceMemberships", boundary: "workspace", scope: "memberships:read"},
		{path: "/api/workspaces/{workspaceId}/members", method: "post", operationID: "addWorkspaceMembership", boundary: "workspace", scope: "memberships:write"},
		{path: "/api/workspaces/{workspaceId}/members/{memberId}", method: "patch", operationID: "updateWorkspaceMembership", boundary: "workspace", scope: "memberships:write"},
		{path: "/api/workspaces/{workspaceId}/members/{memberId}", method: "delete", operationID: "removeWorkspaceMembership", boundary: "workspace", scope: "memberships:write"},
	}

	for _, expectation := range expectations {
		t.Run(expectation.operationID, func(t *testing.T) {
			path := mustMap(t, paths[expectation.path])
			operation := mustMap(t, path[expectation.method])
			assert.Equal(t, expectation.operationID, operation["operationId"])
			assert.Equal(t, "beta", operation["x-stability-level"])
			assert.Equal(t, expectation.boundary, operation["x-kubeorch-workspace-boundary"])
			assert.Equal(t, []any{expectation.scope}, operation["x-kubeorch-required-scopes"])
			assert.Equal(t, []any{map[string]any{"bearerAuth": []any{}}}, operation["security"])
		})
	}
}

func TestWorkspaceJSONMutationsDeclareRequestSizeErrors(t *testing.T) {
	data, err := os.ReadFile("openapi.yaml")
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal(data, &document))
	paths := mustMap(t, document["paths"])

	for _, target := range []struct{ path, method string }{
		{path: "/api/workspaces", method: "post"},
		{path: "/api/workspaces/{workspaceId}", method: "patch"},
		{path: "/api/workspaces/{workspaceId}/members", method: "post"},
		{path: "/api/workspaces/{workspaceId}/members/{memberId}", method: "patch"},
	} {
		operation := mustMap(t, mustMap(t, paths[target.path])[target.method])
		responses := mustMap(t, operation["responses"])
		assert.NotEmpty(t, responses["413"])
	}
}

func TestWorkspaceMutationContractDeclaresIdempotency(t *testing.T) {
	data, err := os.ReadFile("openapi.yaml")
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal(data, &document))
	paths := mustMap(t, document["paths"])

	create := mustMap(t, mustMap(t, paths["/api/workspaces"])["post"])
	assert.Equal(t, "required", create["x-kubeorch-idempotency"])

	for _, target := range []struct{ path, method string }{
		{path: "/api/workspaces/{workspaceId}", method: "patch"},
		{path: "/api/workspaces/{workspaceId}/members", method: "post"},
		{path: "/api/workspaces/{workspaceId}/members/{memberId}", method: "patch"},
		{path: "/api/workspaces/{workspaceId}/members/{memberId}", method: "delete"},
	} {
		operation := mustMap(t, mustMap(t, paths[target.path])[target.method])
		assert.Equal(t, "inherent", operation["x-kubeorch-idempotency"])
	}
}

func mustMap(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	require.True(t, ok, "expected map, got %T", value)
	return result
}
