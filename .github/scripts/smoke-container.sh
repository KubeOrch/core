#!/usr/bin/env bash

set -Eeuo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <image> <amd64|arm64>" >&2
  exit 2
fi

image="$1"
expected_arch="$2"

case "$expected_arch" in
  amd64 | arm64) ;;
  *)
    echo "unsupported architecture: $expected_arch" >&2
    exit 2
    ;;
esac

suffix="${expected_arch}-${RANDOM}-${RANDOM}"
network="kubeorch-smoke-${suffix}"
mongo_container="kubeorch-smoke-mongo-${suffix}"
core_container="kubeorch-smoke-core-${suffix}"
binary_container="kubeorch-smoke-binary-${suffix}"
binary_dir="$(mktemp -d)"
binary_path="${binary_dir}/kubeorch-core"

cleanup() {
  docker rm --force \
    "$core_container" "$mongo_container" "$binary_container" \
    >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
  rm -f "$binary_path"
  rmdir "$binary_dir" >/dev/null 2>&1 || true
}
trap cleanup EXIT

actual_os="$(docker image inspect "$image" --format '{{.Os}}')"
actual_arch="$(docker image inspect "$image" --format '{{.Architecture}}')"
if [[ "$actual_os/$actual_arch" != "linux/$expected_arch" ]]; then
  echo "expected linux/$expected_arch image, got $actual_os/$actual_arch" >&2
  exit 1
fi

docker create \
  --name "$binary_container" \
  --platform "linux/$expected_arch" \
  "$image" >/dev/null
docker cp "$binary_container:/app/kubeorch-core" "$binary_path"

binary_description="$(file --brief "$binary_path")"
case "$expected_arch" in
  amd64)
    expected_binary_pattern='x86-64'
    ;;
  arm64)
    expected_binary_pattern='ARM aarch64'
    ;;
esac
if [[ "$binary_description" != *"$expected_binary_pattern"* ]]; then
  echo "unexpected Core binary: $binary_description" >&2
  exit 1
fi

configured_user="$(docker image inspect "$image" --format '{{.Config.User}}')"
if [[ "$configured_user" != "appuser" ]]; then
  echo "expected image user appuser, got ${configured_user:-<empty>}" >&2
  exit 1
fi

runtime_uid="$(
  docker run --rm \
    --platform "linux/$expected_arch" \
    --entrypoint id \
    "$image" -u
)"
if [[ "$runtime_uid" == "0" ]]; then
  echo "image runs as root" >&2
  exit 1
fi

docker network create "$network" >/dev/null
docker run --detach \
  --name "$mongo_container" \
  --network "$network" \
  mongo:8.0 >/dev/null

mongo_ready=false
for _ in $(seq 1 45); do
  if docker exec "$mongo_container" \
    mongosh --quiet --eval 'quit(db.runCommand({ping: 1}).ok ? 0 : 1)' \
    >/dev/null 2>&1; then
    mongo_ready=true
    break
  fi
  sleep 2
done

if [[ "$mongo_ready" != true ]]; then
  echo "MongoDB did not become ready" >&2
  docker logs "$mongo_container" >&2
  exit 1
fi

docker run --detach \
  --name "$core_container" \
  --network "$network" \
  --platform "linux/$expected_arch" \
  --env "KUBEORCH_MONGO_URI=mongodb://${mongo_container}:27017/kubeorch" \
  --env KUBEORCH_GIN_MODE=release \
  "$image" >/dev/null

core_ready=false
for _ in $(seq 1 45); do
  if ! docker inspect "$core_container" >/dev/null 2>&1; then
    break
  fi

  if docker exec "$core_container" \
    wget --quiet --output-document=- http://127.0.0.1:3000/v1 \
    2>/dev/null | grep --quiet '"status":"success"'; then
    core_ready=true
    break
  fi

  if [[ "$(docker inspect "$core_container" --format '{{.State.Running}}')" != "true" ]]; then
    break
  fi
  sleep 2
done

if [[ "$core_ready" != true ]]; then
  echo "Core did not reach its health endpoint" >&2
  docker logs "$core_container" >&2
  exit 1
fi

echo "Smoke test passed for linux/$expected_arch ($binary_description) as uid $runtime_uid"
