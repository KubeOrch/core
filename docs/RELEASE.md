# Core Container Releases

KubeOrch Core release tags publish one OCI image index for these supported
platforms:

- `linux/amd64`
- `linux/arm64`

The `Release` workflow accepts `v*` tags whose commit is contained in `main`.
It builds and smoke-tests both platform images before publishing the versioned
image. Each smoke test verifies the image architecture, the non-root runtime,
MongoDB-backed startup, and the `/v1` endpoint.

## Published Evidence

The versioned image index includes BuildKit-generated SPDX SBOM and SLSA
provenance attestations. The workflow also creates a GitHub build attestation
for the immutable index digest. After all evidence checks pass, the same index
is promoted to `latest` and its digest is recorded in the GitHub Release.

Use the digest from the GitHub Release instead of trusting a mutable tag:

```bash
IMAGE=ghcr.io/kubeorch/core@sha256:<release-digest>
SOURCE_COMMIT=<release-source-commit>

docker buildx imagetools inspect "$IMAGE"
docker buildx imagetools inspect "$IMAGE" --format '{{ json .SBOM }}'
docker buildx imagetools inspect "$IMAGE" --format '{{ json .Provenance.SLSA }}'
gh attestation verify "oci://$IMAGE" \
  --repo KubeOrch/core \
  --bundle-from-oci \
  --signer-workflow KubeOrch/core/.github/workflows/docker-publish.yml \
  --source-digest "$SOURCE_COMMIT"
```

The manifest list can also be checked mechanically. Attestation descriptors
use `unknown/unknown`; filter those descriptors when asserting runnable image
platforms.

```bash
docker buildx imagetools inspect "$IMAGE" --raw \
  | jq -r '.manifests[] | .platform | select(.os != "unknown") | "\(.os)/\(.architecture)"'
```

The output must contain `linux/amd64` and `linux/arm64`.

## Failure Diagnosis

The release stops before publication if either architecture cannot build or
pass its container smoke test. Logs identify the failing platform and include
the Core or MongoDB container log only on failure. The smoke environment uses
no production credentials.

If publication succeeds but manifest or attestation verification fails, the
workflow does not update `latest` or create the GitHub Release. Inspect the
`Verify platforms and attestations` step for the unexpected platform set,
missing SBOM/provenance data, source revision mismatch, or GitHub attestation
verification error.
