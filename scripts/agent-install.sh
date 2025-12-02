#!/usr/bin/env sh
set -e

echo "[peer-wan] bootstrap starting..."

usage() {
  cat <<EOF
Usage: $0 [--controller=http://ctrl:8080] [--node-id=edge-1] [--provision-token=pt-xxx]
          [--token=jwt] [--plan-interval=30s] [--health-interval=30s]
          [--apply=true|false] [--release-tag=vX.Y.Z] [--proxy=http://host:port]
          [--no-service] [--no-deps]

Flags override env vars (CONTROLLER_ADDR, NODE_ID, PROVISION_TOKEN, TOKEN, PLAN_INTERVAL, HEALTH_INTERVAL, APPLY, RELEASE_TAG, PROXY, SERVICE, AUTO_INSTALL_DEPS).
EOF
}

# defaults
CONTROLLER_ADDR=${CONTROLLER_ADDR:-http://127.0.0.1:8080}
TOKEN=${TOKEN:-changeme}
NODE_ID=${NODE_ID:-edge-1}
PROVISION_TOKEN=${PROVISION_TOKEN:-}
HEALTH_INTERVAL=${HEALTH_INTERVAL:-30s}
PLAN_INTERVAL=${PLAN_INTERVAL:-30s}
APPLY=${APPLY:-true}
RELEASE_TAG=${RELEASE_TAG:-}
ARCH=${ARCH:-$(uname -m)}
SERVICE=${SERVICE:-true}
AUTO_INSTALL_DEPS=${AUTO_INSTALL_DEPS:-true}
PROXY=${PROXY:-}

while [ $# -gt 0 ]; do
  case "$1" in
    --controller=*) CONTROLLER_ADDR="${1#*=}" ;;
    --node-id=*|--id=*) NODE_ID="${1#*=}" ;;
    --provision-token=*) PROVISION_TOKEN="${1#*=}" ;;
    --token=*) TOKEN="${1#*=}" ;;
    --plan-interval=*) PLAN_INTERVAL="${1#*=}" ;;
    --health-interval=*) HEALTH_INTERVAL="${1#*=}" ;;
    --apply=*) APPLY="${1#*=}" ;;
    --release-tag=*) RELEASE_TAG="${1#*=}" ;;
    --proxy=*) PROXY="${1#*=}" ;;
    --no-service) SERVICE=false ;;
    --no-deps) AUTO_INSTALL_DEPS=false ;;
    -h|--help) usage; exit 0 ;;
    *) echo "[peer-wan][warn] unknown flag $1";;
  esac
  shift
done

if [ -z "${PROVISION_TOKEN}" ]; then
  echo "[peer-wan][error] PROVISION_TOKEN is required (get it from controller UI / 添加节点弹窗)"
  exit 1
fi
echo "[peer-wan] CONTROLLER_ADDR=${CONTROLLER_ADDR}"
echo "[peer-wan] NODE_ID=${NODE_ID}"
echo "[peer-wan] PROVISION_TOKEN length=$(echo -n "${PROVISION_TOKEN}" | wc -c)"
echo "[peer-wan] PLAN_INTERVAL=${PLAN_INTERVAL} HEALTH_INTERVAL=${HEALTH_INTERVAL} APPLY=${APPLY} SERVICE=${SERVICE}"

BIN_DIR=${BIN_DIR:-/usr/local/bin}
mkdir -p "${BIN_DIR}"
TMP_DIR=$(mktemp -d)
echo "[peer-wan] BIN_DIR=${BIN_DIR}"
echo "[peer-wan] TMP_DIR=${TMP_DIR}"
echo "[peer-wan] resolving release tag..."

# optional proxy
if [ -n "${PROXY}" ]; then
  export https_proxy="${PROXY}" http_proxy="${PROXY}"
  echo "[peer-wan] using proxy ${PROXY} for downloads"
fi

# clean previous service if present
if [ -f /etc/systemd/system/peer-wan-agent.service ]; then
  echo "[peer-wan] removing old systemd service"
  systemctl stop peer-wan-agent.service || true
  systemctl disable peer-wan-agent.service || true
  rm -f /etc/systemd/system/peer-wan-agent.service
  systemctl daemon-reload || true
fi

# optional dependencies install (wireguard + frr)
if [ "${AUTO_INSTALL_DEPS}" = "true" ]; then
  echo "[peer-wan] installing dependencies (wireguard/frr) if possible..."
  if command -v apt-get >/dev/null 2>&1; then
    sudo apt-get update && sudo apt-get install -y wireguard wireguard-tools frr || echo "[peer-wan][warn] apt install failed, please install wireguard/frr manually"
  elif command -v yum >/dev/null 2>&1; then
    sudo yum install -y epel-release || true
    sudo yum install -y wireguard-tools frr || echo "[peer-wan][warn] yum install failed, please install wireguard/frr manually"
  elif command -v dnf >/dev/null 2>&1; then
    sudo dnf install -y wireguard-tools frr || echo "[peer-wan][warn] dnf install failed, please install wireguard/frr manually"
  else
    echo "[peer-wan][warn] unknown package manager, please install wireguard/frr manually"
  fi
fi

# resolve release tag
if [ -z "${RELEASE_TAG}" ]; then
  HTTP_STATUS=$(curl -w "%{http_code}" -fsSL https://api.github.com/repos/NiuStar/peer-wan/releases/latest -o "${TMP_DIR}/latest.json") || HTTP_STATUS=$?
  if [ "${HTTP_STATUS}" = "200" ]; then
    RELEASE_TAG=$(grep -m1 '"tag_name"' "${TMP_DIR}/latest.json" | sed -E 's/.*"([^"]+)".*/\1/')
  else
    echo "[peer-wan][warn] GitHub API returned ${HTTP_STATUS}, trying redirect fallback..."
    FALLBACK_URL=$(curl -fsSLI -o /dev/null -w '%{url_effective}' https://github.com/NiuStar/peer-wan/releases/latest) || true
    if echo "${FALLBACK_URL}" | grep -q "/tag/"; then
      RELEASE_TAG=$(echo "${FALLBACK_URL}" | sed -E 's#.*/tag/([^/]+).*#\1#')
    fi
  fi
fi
if [ -z "${RELEASE_TAG}" ]; then
  echo "[peer-wan][error] could not resolve release tag; set RELEASE_TAG=vX.Y.Z manually. If behind proxy, set PROXY=http://host:port"
  exit 1
fi
echo "[peer-wan] using release tag: ${RELEASE_TAG}"

to_goarch() {
  case "$1" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    armv7l|armv7) echo "armv7" ;;
    i386|i686) echo "386" ;;
    riscv64) echo "riscv64" ;;
    s390x) echo "s390x" ;;
    loongarch64) echo "loong64" ;;
    *) echo "amd64" ;;
  esac
}

GOARCH=$(to_goarch "${ARCH}")
echo "[peer-wan] detected ARCH=${ARCH} -> GOARCH=${GOARCH}"

candidate_names="
agent-linux-${GOARCH}
agent-linux_${GOARCH}
agent-${GOARCH}
agent_${GOARCH}
"

download_ok=0
for name in $candidate_names; do
  DOWNLOAD_URL="https://github.com/NiuStar/peer-wan/releases/download/${RELEASE_TAG}/${name}"
  echo "[peer-wan] trying download ${DOWNLOAD_URL}"
  if command -v curl >/dev/null 2>&1; then
    if curl -fL --progress-bar "${DOWNLOAD_URL}" -o "${TMP_DIR}/agent"; then
      download_ok=1
      echo "[peer-wan] downloaded ${name} via curl"
      break
    else
      echo "[peer-wan][warn] curl failed for ${DOWNLOAD_URL}"
    fi
  elif command -v wget >/dev/null 2>&1; then
    if wget -O "${TMP_DIR}/agent" "${DOWNLOAD_URL}"; then
      download_ok=1
      echo "[peer-wan] downloaded ${name} via wget"
      break
    else
      echo "[peer-wan][warn] wget failed for ${DOWNLOAD_URL}"
    fi
  else
    echo "[peer-wan][error] curl or wget required to download agent"
    exit 1
  fi
done

if [ "${download_ok}" -ne 1 ]; then
  echo "[peer-wan][error] download failed for all candidate names. Checked:"
  echo "${candidate_names}"
  echo "[peer-wan][hint] verify release ${RELEASE_TAG} assets naming."
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

# install systemd service if available
if [ "${SERVICE}" = "true" ] && command -v systemctl >/dev/null 2>&1; then
  SERVICE_FILE=/etc/systemd/system/peer-wan-agent.service
  echo "[peer-wan] installing systemd service at ${SERVICE_FILE}"
  cat > "${SERVICE_FILE}" <<EOF
[Unit]
Description=peer-wan agent
After=network.target

[Service]
Type=simple
Environment=CONTROLLER_ADDR=${CONTROLLER_ADDR}
Environment=NODE_ID=${NODE_ID}
Environment=PROVISION_TOKEN=${PROVISION_TOKEN}
Environment=TOKEN=${TOKEN}
Environment=PLAN_INTERVAL=${PLAN_INTERVAL}
Environment=HEALTH_INTERVAL=${HEALTH_INTERVAL}
Environment=APPLY=${APPLY}
ExecStart=${BIN_DIR}/peer-wan-agent
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable peer-wan-agent.service
  systemctl restart peer-wan-agent.service
  systemctl status --no-pager peer-wan-agent.service || true
  echo "[peer-wan] systemd service installed and started."
else
  echo "[peer-wan] systemd not installed or SERVICE!=true; skip service install."
fi
