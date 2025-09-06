#!/bin/bash
cd $(git rev-parse --show-toplevel)
docker build -t haloyd-dev -f ./dev/haloyd/Dockerfile.dev .
