#!/bin/bash
set -e

# Ensure an argument is provided
if [ -z "$1" ]; then
    echo "Usage: $0 <hostname>"
    exit 1
fi

CLI_BINARY_NAME=haloy
CLI_ADM_BINARY_NAME=haloyadm

HOSTNAME=$1

# Use the current username from the shell
USERNAME=$(whoami)

# Extract the version from [version.go](http://_vscodecontentref_/1) (assumes format: var Version = "v0.1.9")
version=$(grep 'var Version' ../internal/version/version.go | sed 's/.*"\(.*\)".*/\1/')
echo "Building version: $version"

# Build the CLI binary from cmd/cli using the extracted version
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags="-X 'github.com/ameistad/haloy/cmd.version=$version'" -o $CLI_BINARY_NAME ../cmd/haloy
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags="-X 'github.com/ameistad/haloy/cmd.version=$version'" -o $CLI_ADM_BINARY_NAME ../cmd/haloyadm

# Support localhost: If HOSTNAME is localhost (or 127.0.0.1), use local commands instead of SSH/SCP.
if [ "$HOSTNAME" = "localhost" ] || [ "$HOSTNAME" = "127.0.0.1" ]; then
    echo "Using local deployment for ${HOSTNAME}"
    mkdir -p /home/${USERNAME}/.local/bin
    cp $CLI_BINARY_NAME /home/${USERNAME}/.local/bin/$CLI_BINARY_NAME
    cp $CLI_ADM_BINARY_NAME /home/${USERNAME}/.local/bin/$CLI_ADM_BINARY_NAME
else
    ssh "${USERNAME}@${HOSTNAME}" "mkdir -p /home/${USERNAME}/.local/bin"
    scp $CLI_BINARY_NAME ${USERNAME}@"$HOSTNAME":/home/${USERNAME}/.local/bin/$CLI_BINARY_NAME
    scp $CLI_ADM_BINARY_NAME ${USERNAME}@"$HOSTNAME":/home/${USERNAME}/.local/bin/$CLI_ADM_BINARY_NAME
fi

# Remove binaries after copying
if [ -f "$CLI_BINARY_NAME" ]; then
    rm "$CLI_BINARY_NAME"
fi

if [ -f "$CLI_ADM_BINARY_NAME" ]; then
    rm "$CLI_ADM_BINARY_NAME"
fi

echo "Successfully uploaded CLI binaries."
