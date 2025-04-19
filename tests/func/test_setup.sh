#!/bin/bash
set -euo pipefail

REGISTRY_NAME="local-registry"
REGISTRY_PORT=5000
IMAGE="gcr.io/k8s-skaffold/skaffold"
LOCAL_IMAGE="localhost:${REGISTRY_PORT}/skaffold"

echo "🔍 Checking if Docker is available..."
if ! command -v docker &> /dev/null; then
    echo "❌ Docker not found. Please install Docker and try again."
    exit 1
fi

echo "📦 Checking if local registry is running..."
if ! docker ps --format '{{.Names}}' | grep -q "^${REGISTRY_NAME}$"; then
    echo "🚀 Starting local registry at localhost:${REGISTRY_PORT}..."
    docker run -d -p ${REGISTRY_PORT}:${REGISTRY_PORT} --name ${REGISTRY_NAME} registry:2 || {
        echo "❌ Failed to start Docker registry"
        exit 1
    }
else
    echo "✅ Local registry already running."
fi

echo "⬇️  Pulling Skaffold image from GCR: ${IMAGE}..."
docker pull ${IMAGE} || {
    echo "❌ Failed to pull Skaffold image from ${IMAGE}"
    exit 1
}

echo "🏷️  Tagging image as ${LOCAL_IMAGE}..."
docker tag ${IMAGE} ${LOCAL_IMAGE}

echo "📤 Pushing image to local registry..."
docker push ${LOCAL_IMAGE} || {
    echo "❌ Failed to push image to local registry"
    exit 1
}

echo "✅ Setup complete. Image available at: ${LOCAL_IMAGE}"
