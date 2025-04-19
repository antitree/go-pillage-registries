#!/bin/bash
set -euo pipefail

REGISTRY_NAME="local-registry"
REGISTRY_PORT=5000
IMAGE="gcr.io/k8s-skaffold/skaffold"
LOCAL_IMAGE="localhost:${REGISTRY_PORT}/skaffold"
KEYS_IMAGE="localhost:${REGISTRY_PORT}/keys"

echo "Checking if Docker is available..."
if ! command -v docker &> /dev/null; then
    echo "Docker not found. Please install Docker and try again."
    exit 1
fi

echo "Checking if local registry is running..."
if ! docker ps --format '{{.Names}}' | grep -q "^${REGISTRY_NAME}$"; then
    echo "Starting local registry at localhost:${REGISTRY_PORT}..."
    docker run -d -p ${REGISTRY_PORT}:${REGISTRY_PORT} --name ${REGISTRY_NAME} registry:2 || {
        echo "Failed to start Docker registry"
        exit 1
    }
else
    echo "Local registry already running."
fi

echo "Pulling Skaffold image from GCR..."
docker pull ${IMAGE} || {
    echo "Failed to pull Skaffold image from ${IMAGE}"
    exit 1
}

echo "Tagging Skaffold image as ${LOCAL_IMAGE}..."
docker tag ${IMAGE} ${LOCAL_IMAGE}

echo "Pushing Skaffold image to local registry..."
docker push ${LOCAL_IMAGE} || {
    echo "Failed to push Skaffold image to local registry"
    exit 1
}

echo "Building example keys image from examples/Dockerfile.keys..."
pushd example > /dev/null
docker build -f Dockerfile.keys -t ${KEYS_IMAGE} . || {
    echo "Failed to build keys image"
    exit 1
}
popd > /dev/null

echo "Pushing keys image to local registry..."
docker push ${KEYS_IMAGE} || {
    echo "Failed to push keys image to local registry"
    exit 1
}

echo "Setup complete. Images available:"
echo "  ${LOCAL_IMAGE}"
echo "  ${KEYS_IMAGE}"
