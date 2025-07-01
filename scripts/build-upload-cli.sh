#!/bin/bash
set -e

# Ensure an argument is provided
if [ -z "$1" ]; then
    echo "Usage: $0 <hostname>"
    exit 1
fi

CLI_BINARY_NAME=haloy
CLI_MANAGER_BINARY_NAME=haloy-manager

HOSTNAME=$1

# Use the current username from the shell
USERNAME=$(whoami)

# Extract the version from internal/version/version.go (assumes format: var Version = "v0.1.9")
version=$(grep 'var Version' ../internal/version/version.go | sed 's/.*"\(.*\)".*/\1/')
echo "Building version: $version"

# Build the CLI binary from cmd/cli using the extracted version
GOOS=linux GOARCH=amd64 go build -ldflags="-X 'github.com/ameistad/haloy/cmd.version=$version'" -o $CLI_BINARY_NAME ../cmd/cli
GOOS=linux GOARCH=amd64 go build -ldflags="-X 'github.com/ameistad/haloy/cmd.version=$version'" -o $CLI_MANAGER_BINARY_NAME ../cmd/climanager

# Ensure remote bin dir exists
ssh "${USERNAME}@${HOSTNAME}" "mkdir -p /home/${USERNAME}/.local/bin"

# Upload the binary via scp using the current username
scp $CLI_BINARY_NAME ${USERNAME}@"$HOSTNAME":/home/${USERNAME}/.local/bin/$CLI_BINARY_NAME
scp $CLI_MANAGER_BINARY_NAME ${USERNAME}@"$HOSTNAME":/home/${USERNAME}/.local/bin/$CLI_MANAGER_BINARY_NAME

# Remove binaries after copying
if [ -f $CLI_BINARY_NAME ]; then
    rm $CLI_BINARY_NAME
fi

if [ -f "$CLI_MANAGER_BINARY_NAME" ]; then
    rm "$CLI_MANAGER_BINARY_NAME"
fi
