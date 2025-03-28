#!/bin/bash
# Setup script for Haloy containers

# Get Docker group ID
DOCKER_GID=$(getent group docker | cut -d: -f3)

# If the command failed or DOCKER_GID is empty, use a default value
if [ -z "$DOCKER_GID" ]; then
  echo "Could not detect Docker group ID, using default value 999"
  DOCKER_GID=999
else
  echo "Detected Docker group ID: $DOCKER_GID"
fi

# Create .env file for docker-compose
cat > .env << EOF
# Docker group ID for giving the manager container access to the Docker socket
DOCKER_GID=$DOCKER_GID

# Set to true to use staging server for testing
LEGO_STAGING=false
EOF

echo "Created .env file with Docker group ID"

# Ensure Docker network exists
echo "Setting up Docker network..."
if docker network ls | grep -q haloy-public; then
  echo "Found existing haloy-public network, removing it..."
  # Check if any containers are using this network and disconnect them
  CONTAINERS=$(docker network inspect -f '{{range .Containers}}{{.Name}} {{end}}' haloy-public 2>/dev/null)
  if [ ! -z "$CONTAINERS" ]; then
    echo "Disconnecting containers from network: $CONTAINERS"
    for container in $CONTAINERS; do
      docker network disconnect -f haloy-public "$container" || true
    done
  fi
  # Remove the network
  docker network rm haloy-public || true
fi

# Create the network fresh (without Compose labels)
echo "Creating haloy-public Docker network..."
docker network create haloy-public
echo "Network created."
