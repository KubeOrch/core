# Plans And Approvals

Plans are immutable, workspace-scoped proposals. They put manual, CI, managed
build, and AI-generated changes through one review contract without creating a
second deployment path. Creating, requesting approval for, approving, or
rejecting a Plan never applies Kubernetes resources, opens a Git pull request,
or mutates an Application.

## State Machine

```text
proposed -> approval-requested -> approved
                               -> rejected
```

Only the approval-request transition and one terminal decision are allowed.
There is no update endpoint. Changed content must be submitted as a new Plan.
All three mutations require an actor-scoped `Idempotency-Key`; an exact replay
returns the original response and audit correlation ID, while key reuse with a
different payload returns a conflict.

## Resource Binding

A Plan binds one desired revision to an Application and its Environment inside
the route workspace. Repository lookups always include `workspace_id`, and Core
rejects an Application/Environment mismatch or an archived Application before
persisting the proposal. Cross-workspace misses use the same non-enumerating
`resource_not_found` response as an unknown identifier.

The immutable proposal stores bounded diff and evidence summaries plus
validation, cost, and policy results. Result references must be credential-free
HTTPS URLs without query strings or fragments. Core records these references
without fetching them during Plan creation.

## Decisions

Any active workspace member may create a Plan or request its approval. Only a
workspace owner or administrator may approve or reject it. A required decision
reason is stored with the actor, timestamp, and stable audit correlation ID.

The Beta API always sets `policy.selfApprovalForbidden` to true from trusted
server policy; a caller-provided value cannot relax it. The Plan creator cannot
approve their own proposal, but may still reject it. Once approved or rejected,
a Plan is terminal; only an exact idempotent replay of the original decision
succeeds.

## Observability

Structured transition logs contain workspace, application, environment, Plan,
actor, state, replay, and audit correlation identifiers. They never contain
diffs, evidence summaries, decision reasons, or referenced document content.
The `kubeorch_plan_decisions_total` metric labels only the decision and whether
the accepted request created or replayed the terminal record.
