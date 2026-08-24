# Artifact And Release Handoff

Artifacts and Releases provide an immutable handoff from external CI or manual
onboarding into the KubeOrch Platform API. Registration records evidence; it
does not build source, fetch evidence, change a workflow, or contact a cluster.

## Artifact Identity

An Artifact identifies one OCI repository and digest. The API accepts familiar
OCI image syntax but removes any mutable tag from the stored identity. A request
containing only a tag is rejected. Source repository, ref, and full commit SHA
are required so later audit events can correlate the image with source.

SBOM, provenance, scan, and CI-run evidence are optional HTTPS references.
KubeOrch validates and stores these references without making network requests.
URLs containing credentials, query parameters, or fragments are rejected to
avoid persisting credential material accidentally.

Within a workspace, an identical normalized image, source, and evidence payload
resolves to the same Artifact even when a caller uses a different idempotency
key. Reusing one idempotency key with different input returns a conflict.

## Release Binding

A Release binds one caller-supplied application revision to one or more
registered Artifact IDs. The route application and every Artifact must belong
to the same workspace. Artifact IDs are treated as a set and stored in canonical
order, so equivalent retry payloads produce the same request identity.

`source` is either `external-ci` or `manual`. External CI releases require a
safe HTTPS `sourceReference` for later audit correlation. `createdBy` records
the authenticated actor independently of the declared source.

Releases are immutable records. Creating or reading one has no dependency on a
Kubernetes client, workflow executor, build service, or deployment engine.

## Pagination And Isolation

Artifact and Release lists use stable descending creation order and opaque
cursors. Release cursors are bound to the route application and cannot be reused
for another application or workspace. Cross-workspace resource references use
the same non-enumerating not-found response as unknown identifiers.
