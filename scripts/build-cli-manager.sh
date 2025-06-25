#!/bin/bash
set -e

BINARY_NAME=haloy

# Use the current username from the shell
USERNAME=$(whoami)

# If haloy-cli exists, remove it
if [ -f haloy-cli ]; then
    rm haloy-cli
fi

# Extract the version from internal/version/version.go (assumes format: var Version = "v0.1.9")
version=$(grep 'var Version' ../internal/version/version.go | sed 's/.*"\(.*\)".*/\1/')
echo "Building version: $version"

# Build the CLI binary from cmd/cli using the extracted version
GOOS=linux GOARCH=amd64 go build -ldflags="-X 'github.com/ameistad/haloy/internal/version.Version=$version'" -o $BINARY_NAME ../cmd/cli

# Ensure local bin dir exists
mkdir -p /home/${USERNAME}/.local/bin

# Move the binary to local bin directory
mv $BINARY_NAME /home/${USERNAME}/.local/bin/$BINARY_NAME

# Build the Docker image from Dockerfile
docker build --platform linux/amd64 -t haloy-manager -t haloy-manager:latest -t ghcr.io/ameistad/haloy-manager:latest -f ../build/manager/Dockerfile ../

echo "Successfully built and installed haloy CLI locally and built Docker image haloy-manager."
