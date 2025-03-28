#!/bin/bash
cd $(git rev-parse --show-toplevel)
docker build -t haloy-manager-dev -f ./dev/manager/Dockerfile.dev .
