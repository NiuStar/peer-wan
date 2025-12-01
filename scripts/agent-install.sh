#!/usr/bin/env sh
set -e

CONTROLLER_ADDR=${CONTROLLER_ADDR:-http://127.0.0.1:8080}
TOKEN=${TOKEN:-changeme}
NODE_ID=${NODE_ID:-edge-1}
OVERLAY_IP=${OVERLAY_IP:-10.10.1.1/32}
ENDPOINTS=${ENDPOINTS:-127.0.0.1:51820}
CIDRS=${CIDRS:-10.10.1.0/24}
ASN=${ASN:-65000}

BIN_DIR=${BIN_DIR:-/usr/local/bin}
TMP_DIR=$(mktemp -d)
echo "Installing peer-wan agent to ${BIN_DIR}..."

# download release binary from GitHub
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "https://github.com/NiuStar/peer-wan/releases/latest/download/agent-linux-amd64" -o "${TMP_DIR}/agent"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "${TMP_DIR}/agent" "https://github.com/NiuStar/peer-wan/releases/latest/download/agent-linux-amd64"
else
  echo "curl or wget required"
  exit 1
fi
chmod +x "${TMP_DIR}/agent"

cat > "${BIN_DIR}/peer-wan-agent" <<EOF
#!/usr/bin/env sh
CONTROLLER_ADDR=${CONTROLLER_ADDR} \\
TOKEN=${TOKEN} \\
NODE_ID=${NODE_ID} \\
OVERLAY_IP=${OVERLAY_IP} \\
ASN=${ASN} \\
${BIN_DIR}/agent \\
  --endpoints="${ENDPOINTS}" \\
  --cidrs="${CIDRS}" \\
  --overlay-ip="${OVERLAY_IP}" \\
  --asn=${ASN} \\
  --controller="${CONTROLLER_ADDR}" \\
  --token="${TOKEN}" \\
  "\$@"
EOF
chmod +x "${BIN_DIR}/peer-wan-agent"
echo "Agent wrapper installed: ${BIN_DIR}/peer-wan-agent"
echo "Note: replace placeholder binary download logic with actual release URL."
