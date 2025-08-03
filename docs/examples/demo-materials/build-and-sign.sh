#!/bin/sh
set -e

# Inputs
REPO_URL="${REPO_URL:-https://github.com/myuser/demo-repo}"
IMAGE_TAG="${IMAGE_TAG:-docker.io/myuser/demo:latest}"

# Clone and build
git clone "$REPO_URL" repo && cd repo
docker build -t "$IMAGE_TAG" .

# Push and sign
docker push "$IMAGE_TAG"
COSIGN_PASSWORD="" cosign sign --key /secrets/signing.key "$IMAGE_TAG"

# Clean up
rm -f /secrets/signing.key /root/.docker/config.json

