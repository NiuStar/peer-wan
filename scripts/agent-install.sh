#!/usr/bin/env sh
set -e

CONTROLLER_ADDR=${CONTROLLER_ADDR:-http://127.0.0.1:8080}
TOKEN=${TOKEN:-changeme}
NODE_ID=${NODE_ID:-edge-1}
PROVISION_TOKEN=${PROVISION_TOKEN:-}
HEALTH_INTERVAL=${HEALTH_INTERVAL:-30s}
PLAN_INTERVAL=${PLAN_INTERVAL:-30s}
APPLY=${APPLY:-true}
RELEASE_TAG=${RELEASE_TAG:-}

if [ -z "${PROVISION_TOKEN}" ]; then
  echo "PROVISION_TOKEN is required (obtain via controller UI)"
  exit 1
fi

BIN_DIR=${BIN_DIR:-/usr/local/bin}
mkdir -p "${BIN_DIR}"
TMP_DIR=$(mktemp -d)
echo "Installing peer-wan agent to ${BIN_DIR}..."

# resolve release tag
if [ -z "${RELEASE_TAG}" ]; then
  echo "Resolving latest release tag..."
  RELEASE_TAG=$(curl -fsSL https://api.github.com/repos/NiuStar/peer-wan/releases/latest 2>/dev/null | grep -m1 '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
fi
if [ -z "${RELEASE_TAG}" ]; then
  echo "Failed to resolve release tag, set RELEASE_TAG=vX.Y.Z and retry."
  exit 1
fi
echo "Using release tag: ${RELEASE_TAG}"

# download release binary from GitHub
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "https://github.com/NiuStar/peer-wan/releases/download/${RELEASE_TAG}/agent-linux-amd64" -o "${TMP_DIR}/agent"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "${TMP_DIR}/agent" "https://github.com/NiuStar/peer-wan/releases/download/${RELEASE_TAG}/agent-linux-amd64"
else
  echo "curl or wget required"
  exit 1
fi
chmod +x "${TMP_DIR}/agent"
install -m 0755 "${TMP_DIR}/agent" "${BIN_DIR}/agent"

cat > "${BIN_DIR}/peer-wan-agent" <<EOF
#!/usr/bin/env sh
CONTROLLER_ADDR=${CONTROLLER_ADDR} \\
TOKEN=${TOKEN} \\
NODE_ID=${NODE_ID} \\
PROVISION_TOKEN=${PROVISION_TOKEN} \\
${BIN_DIR}/agent \\
  --controller="${CONTROLLER_ADDR}" \\
  --id="${NODE_ID}" \\
  --provision-token="${PROVISION_TOKEN}" \\
  --token="${TOKEN}" \\
  --auto-endpoint=true \\
  --plan-interval=${PLAN_INTERVAL} \\
  --health-interval=${HEALTH_INTERVAL} \\
  --apply=${APPLY} \\
  "\$@"
EOF
chmod +x "${BIN_DIR}/peer-wan-agent"
echo "Agent wrapper installed: ${BIN_DIR}/peer-wan-agent"
echo "Note: replace placeholder binary download logic with actual release URL."
