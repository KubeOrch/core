# Workspace And Membership Model

Workspaces are the tenancy boundary for new KubeOrch Platform API resources.
Existing user-owned routes remain unchanged until their dedicated migration
issues are implemented.

## Bootstrap

`POST /v1/api/workspaces` atomically creates the workspace and one active
`owner` membership for the authenticated creator. The operation requires an
`Idempotency-Key`. Repeating the key with the same normalized name and
description returns the original workspace; reusing it with different input
returns `idempotency_conflict`.

Workspace memberships are stored with the workspace aggregate. This keeps
workspace creation, owner promotion, owner demotion, and owner removal as
single-document MongoDB writes. The invariant that every workspace has at
least one owner therefore does not depend on replica-set transactions.

## Base Roles

- `owner` can update workspace metadata and manage every base role.
- `admin` can update metadata and manage `admin` and `member` memberships.
- `member` can read workspace metadata and the active membership list.

Only owners can grant, demote, or remove an owner. An operation that would
remove or demote the final owner returns `last_owner_required`.

Adding a user who already has the requested role returns the existing
membership. Adding the same user with a different role returns
`membership_exists`; callers must use the membership role endpoint for that
change.

## Request Validation

Workspace and membership JSON mutation bodies are limited to 32 KiB. Larger
bodies return `request_too_large` with HTTP 413. Workspace name and description
limits count Unicode code points after surrounding whitespace is removed.

Workspace-list cursors are bound to the authenticated identity. Membership-list
cursors are bound to their workspace and continue from the encoded keyset
position even if the member at that position is later removed. A cursor from a
different list or workspace is rejected as invalid.

## Security Boundary

The canonical workspace identity is always the `{workspaceId}` route
parameter. Workspace reads return the same non-enumerating `resource_not_found`
response for an unknown workspace and a workspace the caller cannot access.
New handlers emit `application/problem+json` errors with a safe
`X-Request-Id`. Structured logs contain resource IDs and roles only; they do
not include user email addresses, credentials, or request bodies.

Invitation delivery, custom roles, SAML, SCIM, groups, billing, environment
permissions, and migration of legacy user-owned resources are intentionally
outside this contract.
