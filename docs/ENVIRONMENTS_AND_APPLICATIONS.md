# Environment And Application Identity

Environments and applications are workspace-scoped Platform API resources.
They establish stable product identities without contacting Kubernetes. A
newly created application is therefore a `draft` even when its environment has
no registered cluster.

## Environment Names

Environment names are unique within one workspace. The uniqueness key is
calculated by trimming leading and trailing whitespace, collapsing each run of
Unicode whitespace to one ASCII space, and applying Unicode lowercase
conversion. The display name keeps its original internal spacing and case.

For example, `Production  EU` and `production eu` identify the same normalized
name in one workspace. The same normalized name may be used in another
workspace.

## Application State

Every application stores both `workspaceId` and `environmentId`. Repository
queries always include the route workspace, so an environment or application
identifier from another workspace produces the same `resource_not_found`
response as an unknown identifier.

`desiredState` is an extensible JSON object for desired metadata and references.
Unknown nested fields are preserved so clients can add desired-state concepts
without losing them during a round trip. It is not a credential store: embedded
passwords, tokens, kubeconfigs, credentials, and Secret values are rejected.
Clients should store references such as `credentialsRef` or `secretRef`.

Desired state is separate from observed state. These APIs do not read a cluster,
apply resources, infer rollout health, or claim that desired metadata exists in
Kubernetes.

## Archive And Listing

Archiving is non-destructive and idempotent. It records `archivedAt`, changes
the application status to `archived`, and keeps the item directly retrievable.
Application lists exclude archived records by default. Set
`includeArchived=true` to include them.

Environment and application lists use opaque cursor pagination. Application
cursors are bound to the workspace, environment filter, and archive filter and
cannot be reused with a different query.
