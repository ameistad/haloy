#!/bin/bash
set -e

# Ensure an argument is provided
if [ -z "$1" ]; then
    echo "Usage: $0 <hostname>"
    exit 1
fi

HOSTNAME=$1

# Use the current username from the shell
USERNAME=$(whoami)

# Remote command to delete the haloy binary and config directory
echo "Cleaning up haloy components on ${USERNAME}@${HOSTNAME}"
ssh ${USERNAME}@"$HOSTNAME" << 'EOF'
  echo "Attempting to remove haloy binary..."
  if rm -f $HOME/haloy; then
    echo "Binary removed successfully."
  else
    echo "Failed to remove binary or binary does not exist."
  fi

  echo "Attempting to remove haloy config directory..."
  if rm -rf $HOME/.config/haloy; then
    echo "Config directory removed successfully."
  else
    echo "Failed to remove config directory or directory does not exist."
  fi
EOF
