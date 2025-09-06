#!/bin/bash

# Generate a version tag based on current timestamp
VERSION=$(date +%Y%m%d%H%M%S)

# Build the image with both latest and versioned tags
docker build -t my-nginx:latest -t my-nginx:v${VERSION} .

echo "Built my-nginx:latest and my-nginx:v${VERSION}"

# Optional: Save the version to a file for the run script to use
echo "${VERSION}" > .current-version

echo "Version ${VERSION} saved to .current-version"
