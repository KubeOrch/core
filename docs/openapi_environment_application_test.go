package docs_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestEnvironmentApplicationContractDeclaresOperationsAndBoundaries(t *testing.T) {
	data, err := os.ReadFile("openapi.yaml")
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal(data, &document))
	paths := mustMap(t, document["paths"])

	expectations := []struct {
		path, method, operationID, scope string
	}{
		{path: "/api/workspaces/{workspaceId}/environments", method: "get", operationID: "listEnvironments", scope: "environments:read"},
		{path: "/api/workspaces/{workspaceId}/environments", method: "post", operationID: "createEnvironment", scope: "environments:write"},
		{path: "/api/workspaces/{workspaceId}/environments/{environmentId}", method: "get", operationID: "getEnvironment", scope: "environments:read"},
		{path: "/api/workspaces/{workspaceId}/environments/{environmentId}", method: "patch", operationID: "updateEnvironment", scope: "environments:write"},
		{path: "/api/workspaces/{workspaceId}/applications", method: "get", operationID: "listApplications", scope: "applications:read"},
		{path: "/api/workspaces/{workspaceId}/applications", method: "post", operationID: "createApplication", scope: "applications:write"},
		{path: "/api/workspaces/{workspaceId}/applications/{applicationId}", method: "get", operationID: "getApplication", scope: "applications:read"},
		{path: "/api/workspaces/{workspaceId}/applications/{applicationId}", method: "patch", operationID: "updateApplication", scope: "applications:write"},
		{path: "/api/workspaces/{workspaceId}/applications/{applicationId}", method: "delete", operationID: "archiveApplication", scope: "applications:write"},
	}

	for _, expectation := range expectations {
		t.Run(expectation.operationID, func(t *testing.T) {
			operation := mustMap(t, mustMap(t, paths[expectation.path])[expectation.method])
			assert.Equal(t, expectation.operationID, operation["operationId"])
			assert.Equal(t, "beta", operation["x-stability-level"])
			assert.Equal(t, "workspace", operation["x-kubeorch-workspace-boundary"])
			assert.Equal(t, []any{expectation.scope}, operation["x-kubeorch-required-scopes"])
			assert.Equal(t, []any{map[string]any{"bearerAuth": []any{}}}, operation["security"])

			successStatus := "200"
			if expectation.method == "post" {
				successStatus = "201"
			}
			success := mustMap(t, mustMap(t, operation["responses"])[successStatus])
			headers := mustMap(t, success["headers"])
			assert.Equal(t, "#/components/headers/RequestId", mustMap(t, headers["X-Request-Id"])["$ref"])
		})
	}
}

func TestEnvironmentApplicationMutationsDeclareSizeAndIdempotency(t *testing.T) {
	data, err := os.ReadFile("openapi.yaml")
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal(data, &document))
	paths := mustMap(t, document["paths"])

	for _, target := range []struct{ path, method, idempotency string }{
		{path: "/api/workspaces/{workspaceId}/environments", method: "post", idempotency: "required"},
		{path: "/api/workspaces/{workspaceId}/environments/{environmentId}", method: "patch", idempotency: "inherent"},
		{path: "/api/workspaces/{workspaceId}/applications", method: "post", idempotency: "required"},
		{path: "/api/workspaces/{workspaceId}/applications/{applicationId}", method: "patch", idempotency: "inherent"},
		{path: "/api/workspaces/{workspaceId}/applications/{applicationId}", method: "delete", idempotency: "inherent"},
	} {
		operation := mustMap(t, mustMap(t, paths[target.path])[target.method])
		assert.Equal(t, target.idempotency, operation["x-kubeorch-idempotency"])
		if target.method != "delete" {
			assert.NotEmpty(t, mustMap(t, operation["responses"])["413"])
		}
	}
}

func TestDesiredStateContractRemainsExtensible(t *testing.T) {
	data, err := os.ReadFile("openapi.yaml")
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal(data, &document))
	schemas := mustMap(t, mustMap(t, document["components"])["schemas"])
	desiredState := mustMap(t, schemas["DesiredState"])

	assert.Equal(t, "object", desiredState["type"])
	assert.Equal(t, true, desiredState["additionalProperties"])
}
