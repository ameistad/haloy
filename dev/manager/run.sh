#!/bin/bash
DOCKER_NETWORK=haloy-public

cd $(git rev-parse --show-toplevel)

CERT_STORAGE=$(mktemp -d /tmp/haloy-cert-storage-XXXXXX)
LOGS=$(mktemp -d /tmp/haloy-logs-XXXXXX)

cleanup() {
  rm -rf "$CERT_STORAGE" "$LOGS"
}
trap cleanup EXIT

if ! docker network inspect "$DOCKER_NETWORK" >/dev/null 2>&1; then
  echo "Creating Docker network: $DOCKER_NETWORK"
  docker network create "$DOCKER_NETWORK"
else
  echo "Docker network $DOCKER_NETWORK already exists"
fi

docker run -it --rm \
  --name haloy-manager-dev \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$CERT_STORAGE":/cert-storage:rw \
  -v "$LOGS":/logs:rw \
  -v $(pwd):/src \
  --network haloy-public \
  -e DRY_RUN=true \
  haloy-manager-dev
