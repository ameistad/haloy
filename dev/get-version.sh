#!/bin/bash
# filepath: /home/andreas/haloy/scripts/get-version.sh

set -e

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Path to constants.go relative to the script directory
CONSTANTS_FILE="$SCRIPT_DIR/../internal/constants/constants.go"

# Extract version using awk - look for Version = "..." pattern
version=$(awk -F'"' '/Version.*=.*"/ {print $2; exit}' "$CONSTANTS_FILE" 2>/dev/null)

# Fallback if version not found
if [ -z "$version" ]; then
    echo "Warning: Could not extract version from constants.go, using 'dev'" >&2
    version="dev"
fi

echo "$version"
