#!/bin/bash
set -e

# Ensure an argument is provided
if [ -z "$1" ]; then
    echo "Usage: $0 <hostname>"
    exit 1
fi

BINARY_NAME=haloy
HOSTNAME=$1

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
GOOS=linux GOARCH=amd64 go build -ldflags="-X 'github.com/ameistad/haloy/cmd.version=$version'" -o $BINARY_NAME ../cmd/cli

# Ensure remote bin dir exists
ssh "${USERNAME}@${HOSTNAME}" "mkdir -p /home/${USERNAME}/.local/bin"

# Upload the binary via scp using the current username
scp $BINARY_NAME ${USERNAME}@"$HOSTNAME":/home/${USERNAME}/.local/bin/$BINARY_NAME

# Remove binary after copying
if [ -f $BINARY_NAME ]; then
    rm $BINARY_NAME
fi
