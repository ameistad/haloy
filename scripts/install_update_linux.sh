#!/bin/bash

set -e

# Allow specifying a different destination directory
DIR="${DIR:-"$HOME/.local/bin"}"
mkdir -p "$DIR"

# Detect OS
OS=$(uname -s)
case "$OS" in
    Linux*)   PLATFORM="linux" ;;
    Darwin*)  PLATFORM="darwin" ;;
    *)        echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)   ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)        echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Get latest version tag from GitHub
GITHUB_LATEST_VERSION=$(curl -sL -H 'Accept: application/json' https://github.com/ameistad/haloy/releases/latest | sed -e 's/.*"tag_name":"\([^"]*\)".*/\1/')
if [ -z "$GITHUB_LATEST_VERSION" ]; then
    echo "Could not determine latest Haloy version."
    exit 1
fi

# Prepare download URL and filename
BINARY_NAME="haloy-${PLATFORM}-${ARCH}"
GITHUB_URL="https://github.com/ameistad/haloy/releases/download/${GITHUB_LATEST_VERSION}/${BINARY_NAME}"

# Download and install
echo "Downloading $BINARY_NAME ($GITHUB_LATEST_VERSION)..."
curl -L -o haloy "$GITHUB_URL"
chmod +x haloy
install -Dm 755 haloy "$DIR/haloy"
rm haloy

echo "Haloy installed to $DIR/haloy"
echo "Make sure $DIR is in your PATH."
