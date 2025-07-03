#!/bin/bash
set -e

# Ensure an argument is provided
if [ -z "$1" ]; then
    echo "Usage: $0 <hostname>"
    exit 1
fi

CLI_BINARY_NAME=haloy
CLI_MANAGER_BINARY_NAME=haloyadm

HOSTNAME=$1

# Use the current username from the shell
USERNAME=$(whoami)

# Extract the version from internal/version/version.go (assumes format: var Version = "v0.1.9")
version=$(grep 'var Version' ../internal/version/version.go | sed 's/.*"\(.*\)".*/\1/')
echo "Building version: $version"

# Build the CLI binary from cmd/cli using the extracted version
GOOS=linux GOARCH=amd64 go build -ldflags="-X 'github.com/ameistad/haloy/cmd.version=$version'" -o $CLI_BINARY_NAME ../cmd/haloy
GOOS=linux GOARCH=amd64 go build -ldflags="-X 'github.com/ameistad/haloy/cmd.version=$version'" -o $CLI_MANAGER_BINARY_NAME ../cmd/haloyadm

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

# Build the Docker image from Dockerfile
docker build --platform linux/amd64 -t haloy-manager -t haloy-manager:latest -t ghcr.io/ameistad/haloy-manager:latest -f ../build/manager/Dockerfile ../

# Save the image to a tarball
docker save -o haloy-manager.tar haloy-manager

# Upload the Docker image tarball via scp to the server's /tmp directory
scp haloy-manager.tar ${USERNAME}@"$HOSTNAME":/tmp/haloy-manager.tar

# Load the Docker image on the remote server and remove the tarball
echo "Loading Docker image on remote server..."
if ssh ${USERNAME}@"$HOSTNAME" "docker load -i /tmp/haloy-manager.tar && rm /tmp/haloy-manager.tar"; then
    echo "Successfully loaded Docker image and removed tarball on remote server."
else
    echo "Warning: There was an issue with loading the Docker image or removing the tarball on the remote server."
    echo "You may need to manually check and clean up /tmp/haloy-manager.tar on ${HOSTNAME}"
fi

# Remove the local tarball
rm haloy-manager.tar
