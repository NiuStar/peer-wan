#!/usr/bin/env sh
set -e

echo "[peer-wan] bootstrap starting..."

CONTROLLER_ADDR=${CONTROLLER_ADDR:-http://127.0.0.1:8080}
TOKEN=${TOKEN:-changeme}
NODE_ID=${NODE_ID:-edge-1}
PROVISION_TOKEN=${PROVISION_TOKEN:-}
HEALTH_INTERVAL=${HEALTH_INTERVAL:-30s}
PLAN_INTERVAL=${PLAN_INTERVAL:-30s}
APPLY=${APPLY:-true}
RELEASE_TAG=${RELEASE_TAG:-}

if [ -z "${PROVISION_TOKEN}" ]; then
  echo "[peer-wan][error] PROVISION_TOKEN is required (get it from controller UI / 添加节点弹窗)"
  exit 1
fi
echo "[peer-wan] CONTROLLER_ADDR=${CONTROLLER_ADDR}"
echo "[peer-wan] NODE_ID=${NODE_ID}"
echo "[peer-wan] PROVISION_TOKEN length=$(echo -n "${PROVISION_TOKEN}" | wc -c)"

BIN_DIR=${BIN_DIR:-/usr/local/bin}
mkdir -p "${BIN_DIR}"
TMP_DIR=$(mktemp -d)
echo "[peer-wan] BIN_DIR=${BIN_DIR}"
echo "[peer-wan] TMP_DIR=${TMP_DIR}"
echo "[peer-wan] resolving release tag..."

# resolve release tag
if [ -z "${RELEASE_TAG}" ]; then
  HTTP_STATUS=$(curl -w "%{http_code}" -fsSL https://api.github.com/repos/NiuStar/peer-wan/releases/latest -o "${TMP_DIR}/latest.json") || HTTP_STATUS=$?
  if [ "${HTTP_STATUS}" != "200" ]; then
    echo "[peer-wan][error] GitHub API returned ${HTTP_STATUS}, body:"
    cat "${TMP_DIR}/latest.json" 2>/dev/null || true
    echo "[peer-wan][hint] set RELEASE_TAG=vX.Y.Z manually and retry."
    exit 1
  fi
  RELEASE_TAG=$(grep -m1 '"tag_name"' "${TMP_DIR}/latest.json" | sed -E 's/.*"([^"]+)".*/\1/')
fi
if [ -z "${RELEASE_TAG}" ]; then
  echo "[peer-wan][error] could not resolve release tag; set RELEASE_TAG=vX.Y.Z and retry"
  exit 1
fi
echo "[peer-wan] using release tag: ${RELEASE_TAG}"

DOWNLOAD_URL="https://github.com/NiuStar/peer-wan/releases/download/${RELEASE_TAG}/agent-linux-amd64"
echo "[peer-wan] downloading agent from ${DOWNLOAD_URL}"
# download release binary from GitHub with verbose progress
if command -v curl >/dev/null 2>&1; then
  if ! curl -fL --progress-bar "${DOWNLOAD_URL}" -o "${TMP_DIR}/agent"; then
    echo "[peer-wan][error] download failed via curl from ${DOWNLOAD_URL}"
    exit 1
  fi
elif command -v wget >/dev/null 2>&1; then
  if ! wget -O "${TMP_DIR}/agent" "${DOWNLOAD_URL}"; then
    echo "[peer-wan][error] download failed via wget from ${DOWNLOAD_URL}"
    exit 1
  fi
else
  echo "[peer-wan][error] curl or wget required to download agent"
  exit 1
fi
chmod +x "${TMP_DIR}/agent"
install -m 0755 "${TMP_DIR}/agent" "${BIN_DIR}/agent"
echo "[peer-wan] agent binary installed to ${BIN_DIR}/agent"

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
echo "[peer-wan] wrapper installed: ${BIN_DIR}/peer-wan-agent"
echo "[peer-wan] run with: sudo peer-wan-agent"
