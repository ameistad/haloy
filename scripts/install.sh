#!/bin/bash

set -e

# The directory to install the binary to. Can be overridden by setting the DIR environment variable.
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

# --- Download and Install ---
BINARY_NAME="haloy-${PLATFORM}-${ARCH}"
DOWNLOAD_URL="https://github.com/ameistad/haloy/releases/download/${GITHUB_LATEST_VERSION}/${BINARY_NAME}"
INSTALL_PATH="$DIR/haloy"

# Create the installation directory if it doesn't exist
mkdir -p "$DIR"

echo "Downloading Haloy ${GITHUB_LATEST_VERSION} for ${PLATFORM}/${ARCH}..."
curl -L -o "$INSTALL_PATH" "$DOWNLOAD_URL"
chmod +x "$INSTALL_PATH"

echo ""
echo "✅ Haloy client has been installed to '$INSTALL_PATH'"
echo ""
echo "Please ensure '$DIR' is in your system's PATH."
echo "You can check by running: 'echo \$PATH'"
echo "If not, add it to your shell's profile (e.g., ~/.bashrc, ~/.zshrc):"
echo "   export PATH=\"\$HOME/.local/bin:\$PATH\""
