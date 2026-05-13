#!/usr/bin/env bash
# Wrapper for the ProxiPort dev container. Builds the image if missing,
# then runs the requested command inside it with the repo bind-mounted.
#
#   ./deploy/docker/dev/run.sh build         # go build ./...
#   ./deploy/docker/dev/run.sh vet           # go vet ./...
#   ./deploy/docker/dev/run.sh test ./...    # forward any go-test args
#   ./deploy/docker/dev/run.sh shell         # interactive shell
#   ./deploy/docker/dev/run.sh -- go env     # any raw command

set -euo pipefail

IMAGE="${PROXIPORT_DEV_IMAGE:-proxiport-dev:latest}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
    echo "[dev] image $IMAGE not found, building..."
    docker build -t "$IMAGE" \
        -f "$REPO_ROOT/deploy/docker/dev/Dockerfile" \
        "$REPO_ROOT"
fi

# Cache GOPATH/Go build cache outside the container so subsequent runs are
# fast. Lives at ~/.cache/proxiport-dev so it doesn't clutter the repo.
CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/proxiport-dev"
mkdir -p "$CACHE_DIR/gopath" "$CACHE_DIR/gocache"

DOCKER_FLAGS=(--rm)
if [ -t 0 ] && [ -t 1 ]; then
    DOCKER_FLAGS+=(-it)
fi

docker_run() {
    docker run "${DOCKER_FLAGS[@]}" \
        -v "$REPO_ROOT":/workspace \
        -v "$CACHE_DIR/gopath":/go \
        -v "$CACHE_DIR/gocache":/root/.cache/go-build \
        -w /workspace \
        "$IMAGE" "$@"
}

case "${1:-build}" in
    build)  shift; docker_run go build ./... "$@" ;;
    vet)    shift; docker_run go vet ./... "$@" ;;
    test)   shift; docker_run go test "$@" ;;
    shell)  shift; docker_run bash ;;
    --)     shift; docker_run "$@" ;;
    *)      docker_run "$@" ;;
esac
