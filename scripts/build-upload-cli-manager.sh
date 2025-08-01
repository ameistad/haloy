#!/bin/bash
set -e

if [ -z "$1" ]; then
    echo "Usage: $0 <hostname>"
    exit 1
fi

# Check if CGO dependencies are available
if ! command -v gcc &> /dev/null; then
    echo "Error: gcc not found. Install build dependencies:"
    echo "  On Debian/Ubuntu: sudo apt install build-essential"
    echo "  On Alpine: apk add gcc musl-dev"
    echo "  On CentOS/RHEL: sudo yum install gcc glibc-devel"
    exit 1
fi

CLI_BINARY_NAME=haloy
CLI_ADM_BINARY_NAME=haloyadm

HOSTNAME=$1
USERNAME=$(whoami)

version=$(grep 'var Version' ../internal/version/version.go | sed 's/.*"\(.*\)".*/\1/')
echo "Building version: $version"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X 'github.com/ameistad/haloy/cmd.version=$version'" -o $CLI_BINARY_NAME ../cmd/haloy
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X 'github.com/ameistad/haloy/cmd.version=$version'" -o $CLI_ADM_BINARY_NAME ../cmd/haloyadm

# Support localhost: If HOSTNAME is localhost, use local commands instead of SSH/SCP,
# otherwise use SSH.
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

if [ -f "$CLI_BINARY_NAME" ]; then
    rm "$CLI_BINARY_NAME"
fi

if [ -f "$CLI_ADM_BINARY_NAME" ]; then
    rm "$CLI_ADM_BINARY_NAME"
fi

docker build --platform linux/amd64 -t haloy-manager -t haloy-manager:latest -t ghcr.io/ameistad/haloy-manager:latest -f ../build/manager/Dockerfile ../

docker save -o haloy-manager.tar haloy-manager

if [ "$HOSTNAME" = "localhost" ] || [ "$HOSTNAME" = "127.0.0.1" ]; then
    echo "Loading Docker image locally..."
    docker load -i haloy-manager.tar && rm haloy-manager.tar
else
    scp haloy-manager.tar ${USERNAME}@"$HOSTNAME":/tmp/haloy-manager.tar
    echo "Loading Docker image on remote server..."
    if ssh ${USERNAME}@"$HOSTNAME" "docker load -i /tmp/haloy-manager.tar && rm /tmp/haloy-manager.tar"; then
        echo "Successfully loaded Docker image and removed tarball on remote server."
    else
        echo "Warning: There was an issue with loading the Docker image or removing the tarball on the remote server."
        echo "You may need to manually check and clean up /tmp/haloy-manager.tar on ${HOSTNAME}"
    fi
    rm haloy-manager.tar
fi
