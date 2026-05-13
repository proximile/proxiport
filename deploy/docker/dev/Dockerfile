# Development toolchain image for ProxiPort.
#
# Builds a minimal Debian + Go environment large enough to compile this
# repo (cgo + sqlite3) and run `go build ./...`, `go vet ./...`, `go test
# ./...`. Not for production — the runtime image lives one directory up.
#
# Usage:
#   docker build -t proxiport-dev:latest deploy/docker/dev
#   docker run --rm -v "$PWD":/workspace -w /workspace proxiport-dev:latest go build ./...
#
# The wrapper script `deploy/docker/dev/run.sh` does both steps for you.

FROM golang:1.22-bookworm

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        git \
        make \
        pkg-config \
 && rm -rf /var/lib/apt/lists/*

ENV CGO_ENABLED=1 \
    GOFLAGS=-buildvcs=false

# The agent reads /etc/machine-id when use_system_id=true; the slim base
# image doesn't ship one, which breaks BDD tests that exercise the default
# client config. A fixed value is fine here — this image is only ever used
# for in-container builds and tests, never as a runtime base.
RUN echo "0123456789abcdef0123456789abcdef" > /etc/machine-id

WORKDIR /workspace

# Cache the Go module download by copying go.mod/go.sum at build time. The
# actual source is bind-mounted at run time so edits don't require a rebuild.
COPY go.mod go.sum* /workspace/
RUN if [ -f go.sum ]; then go mod download; fi

CMD ["bash"]
