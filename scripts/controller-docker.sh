#!/usr/bin/env bash
set -euo pipefail

TAG=${TAG:-latest}
TOKEN=${TOKEN:-changeme}
CONSUL_HTTP_ADDR=${CONSUL_HTTP_ADDR:-http://consul:8500}
PROJECT_ROOT=$(cd "$(dirname "$0")/.." && pwd)

echo "Building controller image (consul-enabled) tag=${TAG}..."
docker build -f "${PROJECT_ROOT}/Dockerfile.controller" -t "peer-wan-controller:${TAG}" "${PROJECT_ROOT}"

echo "Starting controller + consul via docker-compose.controller.yaml..."
cd "${PROJECT_ROOT}"
docker compose -f docker-compose.controller.yaml up -d

echo "Controller running at http://localhost:8080 (token=${TOKEN}); UI at /ui/; Consul UI at http://localhost:8500"
echo "To update: rerun this script; docker will rebuild and restart containers."
