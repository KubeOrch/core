package openapicheck

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRepositoryContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "openapi.yaml"))
	require.NoError(t, err)

	violations, err := Validate(data, "docs/openapi.yaml")
	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestValidateConventions(t *testing.T) {
	t.Run("accepts the complete fixture", func(t *testing.T) {
		data := readFixture(t, "base.yaml")

		violations, err := Validate(data, "base.yaml")

		require.NoError(t, err)
		assert.Empty(t, violations)
	})

	t.Run("requires scope metadata on protected operations", func(t *testing.T) {
		data := []byte(`openapi: 3.0.3
info:
  title: Invalid API
  version: 1.0.0
paths:
  /api/items:
    get:
      operationId: listItems
      x-stability-level: beta
      x-kubeorch-workspace-boundary: identity
      security:
        - bearerAuth: []
      responses:
        "200":
          description: Items
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
`)

		violations, err := Validate(data, "invalid.yaml")

		require.NoError(t, err)
		require.Len(t, violations, 1)
		assert.Contains(t, violations[0].Message, scopesExtension)
	})

	t.Run("requires complete deprecation metadata", func(t *testing.T) {
		data := []byte(`openapi: 3.0.3
info:
  title: Invalid API
  version: 1.0.0
paths:
  /api/status:
    get:
      operationId: getStatus
      deprecated: true
      x-stability-level: beta
      x-kubeorch-workspace-boundary: none
      responses:
        "200":
          description: Status
`)

		violations, err := Validate(data, "deprecated.yaml")

		require.NoError(t, err)
		assert.Len(t, violations, 3)
	})
}

func TestCompatibilityClassification(t *testing.T) {
	base := readFixture(t, "base.yaml")

	t.Run("rejects a removed operation and response field", func(t *testing.T) {
		changes, err := Compare(base, "base.yaml", readFixture(t, "breaking.yaml"), "breaking.yaml")

		require.NoError(t, err)
		assert.NotEmpty(t, changes)
		assert.Contains(t, changeIDs(changes), "api-removed-without-deprecation")
		assert.Contains(t, changeIDs(changes), "response-required-property-removed")
	})

	t.Run("accepts additive operations and optional fields", func(t *testing.T) {
		revision := readFixture(t, "additive.yaml")
		violations, err := Validate(revision, "additive.yaml")
		require.NoError(t, err)
		assert.Empty(t, violations)

		changes, err := Compare(base, "base.yaml", revision, "additive.yaml")

		require.NoError(t, err)
		assert.Empty(t, changes)
	})

	t.Run("rejects a new protected legacy-user mutation", func(t *testing.T) {
		revision := readFixture(t, "new-legacy-operation.yaml")
		violations, err := Validate(revision, "new-legacy-operation.yaml")
		require.NoError(t, err)
		assert.Empty(t, violations)

		changes, err := Compare(
			readFixture(t, "empty.yaml"),
			"empty.yaml",
			revision,
			"new-legacy-operation.yaml",
		)

		require.NoError(t, err)
		assert.Contains(t, changeIDs(changes), "new-operation-uses-legacy-user-boundary")
		assert.Contains(t, changeIDs(changes), "new-protected-mutation-missing-idempotency")
	})

	t.Run("rejects an optional required idempotency header", func(t *testing.T) {
		revision := readFixture(t, "new-optional-idempotency.yaml")
		violations, err := Validate(revision, "new-optional-idempotency.yaml")
		require.NoError(t, err)
		require.Len(t, violations, 1)
		assert.Contains(t, violations[0].Message, "required idempotency")

		changes, err := Compare(
			readFixture(t, "empty.yaml"),
			"empty.yaml",
			revision,
			"new-optional-idempotency.yaml",
		)

		require.NoError(t, err)
		assert.Contains(t, changeIDs(changes), "new-protected-mutation-invalid-idempotency")
	})

	t.Run("adopts beta for a previously unannotated operation", func(t *testing.T) {
		changes, err := Compare(
			readFixture(t, "unannotated.yaml"),
			"unannotated.yaml",
			readFixture(t, "beta-adoption.yaml"),
			"beta-adoption.yaml",
		)

		require.NoError(t, err)
		assert.Empty(t, changes)
	})

	t.Run("rejects an explicit stability downgrade", func(t *testing.T) {
		changes, err := Compare(
			readFixture(t, "beta-adoption.yaml"),
			"beta-adoption.yaml",
			readFixture(t, "alpha-downgrade.yaml"),
			"alpha-downgrade.yaml",
		)

		require.NoError(t, err)
		assert.Contains(t, changeIDs(changes), "api-stability-decreased")
	})
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return data
}

func changeIDs(changes []BreakingChange) []string {
	result := make([]string, 0, len(changes))
	for _, change := range changes {
		result = append(result, change.ID)
	}
	return result
}
