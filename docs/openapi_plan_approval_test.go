package docs_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestPlanApprovalContractDeclaresOperationsBoundariesAndScopes(t *testing.T) {
	document := readPlanContract(t)
	paths := mustMap(t, document["paths"])
	expectations := []struct {
		path, method, operationID, scope, successStatus string
	}{
		{path: "/api/workspaces/{workspaceId}/plans", method: "get", operationID: "listPlans", scope: "plans:read", successStatus: "200"},
		{path: "/api/workspaces/{workspaceId}/plans", method: "post", operationID: "createPlan", scope: "plans:write", successStatus: "201"},
		{path: "/api/workspaces/{workspaceId}/plans/{planId}", method: "get", operationID: "getPlan", scope: "plans:read", successStatus: "200"},
		{path: "/api/workspaces/{workspaceId}/plans/{planId}/approval-requests", method: "post", operationID: "requestPlanApproval", scope: "plans:write", successStatus: "201"},
		{path: "/api/workspaces/{workspaceId}/plans/{planId}/decisions", method: "post", operationID: "decidePlan", scope: "plans:approve", successStatus: "201"},
	}

	for _, expectation := range expectations {
		t.Run(expectation.operationID, func(t *testing.T) {
			operation := mustMap(t, mustMap(t, paths[expectation.path])[expectation.method])
			assert.Equal(t, expectation.operationID, operation["operationId"])
			assert.Equal(t, "beta", operation["x-stability-level"])
			assert.Equal(t, "workspace", operation["x-kubeorch-workspace-boundary"])
			assert.Equal(t, []any{expectation.scope}, operation["x-kubeorch-required-scopes"])
			assert.Equal(t, []any{map[string]any{"bearerAuth": []any{}}}, operation["security"])
			success := mustMap(t, mustMap(t, operation["responses"])[expectation.successStatus])
			headers := mustMap(t, success["headers"])
			assert.Equal(t, "#/components/headers/RequestId", mustMap(t, headers["X-Request-Id"])["$ref"])
		})
	}
}

func TestPlanMutationsAreIdempotentAndNeverApply(t *testing.T) {
	document := readPlanContract(t)
	paths := mustMap(t, document["paths"])
	for _, path := range []string{
		"/api/workspaces/{workspaceId}/plans",
		"/api/workspaces/{workspaceId}/plans/{planId}/approval-requests",
		"/api/workspaces/{workspaceId}/plans/{planId}/decisions",
	} {
		operation := mustMap(t, mustMap(t, paths[path])["post"])
		assert.Equal(t, "required", operation["x-kubeorch-idempotency"])
		parameters := operation["parameters"].([]any)
		assert.Contains(t, parameters, map[string]any{"$ref": "#/components/parameters/IdempotencyKey"})
		assert.NotEmpty(t, mustMap(t, operation["responses"])["413"])
		assert.Contains(t, strings.ToLower(operation["description"].(string)), "does not apply")
	}

	for path := range paths {
		if strings.Contains(path, "/plans") {
			assert.NotContains(t, path, "/apply")
		}
	}
}

func TestPlanSchemaIsImmutableBoundedAndAuditCorrelated(t *testing.T) {
	document := readPlanContract(t)
	schemas := mustMap(t, mustMap(t, document["components"])["schemas"])
	plan := mustMap(t, schemas["Plan"])
	properties := mustMap(t, plan["properties"])
	required := plan["required"].([]any)

	assert.Equal(t, false, plan["additionalProperties"])
	assert.Contains(t, required, "workspaceId")
	assert.Contains(t, required, "applicationId")
	assert.Contains(t, required, "environmentId")
	assert.Contains(t, required, "auditCorrelationId")
	assert.NotContains(t, properties, "apply")
	assert.NotContains(t, properties, "appliedAt")
	assert.Equal(t, []any{"proposed", "approval-requested", "approved", "rejected"}, mustMap(t, schemas["PlanStatus"])["enum"])
	assert.Equal(t, []any{"approve", "reject"}, mustMap(t, mustMap(t, schemas["CreatePlanDecisionRequest"])["properties"])["decision"].(map[string]any)["enum"])

	reference := mustMap(t, schemas["PlanEvidenceReference"])
	assert.Equal(t, "uri", reference["format"])
	assert.Contains(t, reference["pattern"], "https://")
	assert.Equal(t, 2048, reference["maxLength"])
	selfApprovalForbidden := mustMap(t, mustMap(t, schemas["PlanPolicyResult"])["properties"])["selfApprovalForbidden"].(map[string]any)
	assert.Equal(t, true, selfApprovalForbidden["default"])
	assert.Equal(t, true, selfApprovalForbidden["readOnly"])
}

func readPlanContract(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("openapi.yaml")
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal(data, &document))
	return document
}
