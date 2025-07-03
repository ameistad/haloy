#!/bin/bash

set -e

# The directory to install the binaries to. Can be overridden by setting the DIR environment variable.
DIR="${DIR:-"$HOME/.local/bin"}"

# --- Auto-detect OS and Architecture ---
OS=$(uname -s)
case "$OS" in
    Linux*)   PLATFORM="linux" ;;
    Darwin*)  PLATFORM="darwin" ;;
    *)        echo "Error: Unsupported OS '$OS'. Haloy supports Linux and macOS." >&2; exit 1 ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
    x86_64)   ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)        echo "Error: Unsupported architecture '$ARCH'. Haloy supports amd64 (x86_64) and arm64." >&2; exit 1 ;;
esac

# --- Fetch the latest version from GitHub ---
echo "Finding the latest version of Haloy..."
GITHUB_API_URL="https://api.github.com/repos/ameistad/haloy/releases/latest"
GITHUB_LATEST_VERSION=$(curl -sL -H 'Accept: application/json' "$GITHUB_API_URL" | grep '"tag_name":' | sed -e 's/.*"tag_name":"\([^"]*\)".*/\1/')

if [ -z "$GITHUB_LATEST_VERSION" ]; then
    echo "Error: Could not determine the latest Haloy version from GitHub." >&2
    exit 1
fi

# Create the installation directory if it doesn't exist
mkdir -p "$DIR"

# --- Download and Install haloy ---
CLIENT_BINARY_NAME="haloy-${PLATFORM}-${ARCH}"
CLIENT_DOWNLOAD_URL="https://github.com/ameistad/haloy/releases/download/${GITHUB_LATEST_VERSION}/${CLIENT_BINARY_NAME}"
CLIENT_INSTALL_PATH="$DIR/haloy"

echo "Downloading Haloy client ${GITHUB_LATEST_VERSION}..."
curl -L -o "$CLIENT_INSTALL_PATH" "$CLIENT_DOWNLOAD_URL"
chmod +x "$CLIENT_INSTALL_PATH"

# --- Download and Install haloyadm ---
ADMIN_BINARY_NAME="haloyadm-${PLATFORM}-${ARCH}"
ADMIN_DOWNLOAD_URL="https://github.com/ameistad/haloy/releases/download/${GITHUB_LATEST_VERSION}/${ADMIN_BINARY_NAME}"
ADMIN_INSTALL_PATH="$DIR/haloyadm"

echo "Downloading Haloy admin tool ${GITHUB_LATEST_VERSION}..."
curl -L -o "$ADMIN_INSTALL_PATH" "$ADMIN_DOWNLOAD_URL"
chmod +x "$ADMIN_INSTALL_PATH"


echo ""
echo "✅ Haloy server tools ('haloy' and 'haloyadm') have been installed to '$DIR'"
echo ""
echo "Please ensure '$DIR' is in your system's PATH."
echo "You can check by running: 'echo \$PATH'"
echo "If not, add it to your shell's profile (e.g., ~/.bashrc, ~/.zshrc):"
echo "   export PATH=\"\$HOME/.local/bin:\$PATH\""
