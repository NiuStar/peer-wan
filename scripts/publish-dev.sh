#!/usr/bin/env bash
set -euo pipefail

REG_CONTROLLER=${REG_CONTROLLER:-24802117/peer-wan-controller}
REG_AGENT=${REG_AGENT:-24802117/peer-wan-agent}
TAG=${TAG:-dev}
PROJECT_ROOT=$(cd "$(dirname "$0")/.." && pwd)

echo "Building controller image (tag=${TAG}) ..."
docker build -f "${PROJECT_ROOT}/Dockerfile.controller" -t "${REG_CONTROLLER}:${TAG}" "${PROJECT_ROOT}"

echo "Building agent image (tag=${TAG}) ..."
docker build -f "${PROJECT_ROOT}/Dockerfile.agent" -t "${REG_AGENT}:${TAG}" "${PROJECT_ROOT}"

echo "Pushing controller image..."
docker push "${REG_CONTROLLER}:${TAG}"

echo "Pushing agent image..."
docker push "${REG_AGENT}:${TAG}"

echo "Done. Pushed:"
echo " - ${REG_CONTROLLER}:${TAG}"
echo " - ${REG_AGENT}:${TAG}"
