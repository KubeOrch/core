# KubeOrch Beta API Conventions

`docs/openapi.yaml` is the source of truth for the KubeOrch Platform API. New
UI, CLI, agent, skill, MCP, and Cloud integrations must be designed against
that contract rather than undocumented handler behavior.

The public prefix remains `/v1/api/`. The `v1` prefix identifies the product
API family; stability is declared per operation with `x-stability-level`.

## Resource Shape

- Resource IDs are opaque strings using `ResourceId`. Clients must not parse an
  ID, infer its storage format, or sort by it.
- JSON properties and path parameters use `lowerCamelCase`.
- Timestamps use `Timestamp`: RFC 3339 in UTC, emitted with a trailing `Z`.
- Resource responses expose stable IDs and timestamps, not database field names.
- Unknown request fields must produce a documented error or be preserved when a
  resource explicitly supports extensible desired state. They must not be
  silently discarded.

## Workspace Boundary

Workspace-scoped endpoints use this canonical form:

```text
/v1/api/workspaces/{workspaceId}/...
```

They reference the reusable `WorkspaceId` path parameter and declare
`x-kubeorch-workspace-boundary: workspace`. A header, query parameter, token
claim, or request body must not override the route workspace.

Every operation declares one of these boundaries:

- `workspace`: a Membership and workspace authorization context are required.
- `identity`: the authenticated identity is establishing or listing workspaces.
- `legacy-user`: an existing user-owned endpoint awaiting workspace migration.
- `none`: the endpoint is intentionally public or has no resource boundary.

`legacy-user` is permitted only for existing routes. New protected operations
must be workspace-scoped unless an approved architecture decision documents a
different global administrative boundary.

## Authentication And Scopes

Protected operations declare their security scheme and a non-empty
`x-kubeorch-required-scopes` list. Scope names use `resource:action`, for
example `applications:read` or `plans:approve`.

The metadata defines the contract consumed by later authorization work. Adding
metadata to a legacy operation does not itself claim that the current handler
already enforces scoped tokens.

Authorization failures must not reveal whether a resource exists in another
workspace. Credentials, token material, Secret values, kubeconfigs, and raw
customer log content must never appear in error details.

## Pagination

Collection endpoints use cursor pagination with the reusable `PageCursor` and
`PageSize` parameters. Responses include `PageInfo` alongside the collection.

- Cursors are opaque and endpoint-specific.
- The default page size is 20 and the maximum is 100.
- Ordering is stable and documented by the endpoint.
- Invalid or expired cursors return an `APIError`; they do not silently restart
  at the first page.

## Errors

New Beta operations return `application/problem+json` using `APIError` and a
safe `X-Request-Id` correlation header. `code` is the stable programmatic
identifier; `title` and `detail` are human-readable and may evolve.

Existing endpoints may retain their legacy `Error` response until their
contract is migrated. New operations must not introduce another error envelope.

## Idempotency

Create and other non-idempotent mutations that can be retried use the required
`IdempotencyKey` header parameter. The key is scoped to the authenticated actor,
workspace, operation, and normalized request payload.

New mutations declare `x-kubeorch-idempotency` as `required`, `inherent`, or
`not-applicable`. `required` operations reference `IdempotencyKey`; `inherent`
operations document why repeating the same request has the same result.

- The first completed result is replayed for the retention window.
- Reusing a key with a different payload returns a conflict.
- Concurrent requests with the same key produce one operation.
- Replayed responses set the `IdempotencyReplayed` header.
- Naturally idempotent updates still document their conflict and retry rules.

## Stability And Deprecation

Every operation has a unique `operationId` and one of these
`x-stability-level` values: `draft`, `alpha`, `beta`, or `stable`.

When stability metadata is first adopted for an existing unannotated operation,
the new annotation establishes its baseline; absence of the extension did not
implicitly promise stable status. After that baseline exists, decreasing an
operation's explicit stability level is a breaking change.

Beta compatibility permits additive operations, optional properties, optional
parameters, and new response codes that do not invalidate an existing success
contract. Removing an operation or field, making input required, narrowing a
schema, or otherwise invalidating a conforming client is a breaking change.

Deprecated operations set standard `deprecated: true` and provide:

- `x-kubeorch-deprecated-at`: UTC date in `YYYY-MM-DD` form.
- `x-kubeorch-sunset-at`: UTC date in `YYYY-MM-DD` form.
- `x-kubeorch-deprecation-reason`: migration guidance or a replacement
  `operationId`.

Breaking Beta changes require a new operation or contract version. They are not
merged by suppressing the compatibility gate.

## Maintainer Workflow

Validate the current contract:

```bash
make openapi-validate
```

Compare it with another Git revision:

```bash
make openapi-compat OPENAPI_BASE=origin/main
```

Pull requests validate the OpenAPI document and compare it with the pull
request's base commit. The fixture tests prove that an additive optional change
passes and a breaking field or operation removal fails.

Contract changes should include the OpenAPI update, compatibility result,
handler tests where runtime behavior changes, consumer rollout notes, and a
deprecation entry when applicable.
