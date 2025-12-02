#!/usr/bin/env bash
set -euo pipefail

# Build cross-platform binaries for controller and agent, then upload to GitHub Release.
# Requirements:
# - Go 1.25+
# - gh CLI logged in (GITHUB_TOKEN or gh auth login)
# Usage:
#   RELEASE_TAG=v0.1.0 scripts/build-release.sh

RELEASE_TAG=${RELEASE_TAG:-}
if [ -z "${RELEASE_TAG}" ]; then
  echo "RELEASE_TAG is required, e.g. RELEASE_TAG=v0.1.0 scripts/build-release.sh"
  exit 1
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="${ROOT}/dist"
mkdir -p "${DIST}"

build_one() {
  local os="$1"
  local arch="$2"
  local extra_env="$3"
  local suffix="${os}_${arch}"
  local outdir="${DIST}/${suffix}"
  mkdir -p "${outdir}"
  echo "Building ${os}/${arch} ${extra_env}"
  env CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" ${extra_env} go build -tags=consul -o "${outdir}/controller-${suffix}" "${ROOT}/cmd/controller"
  env CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" ${extra_env} go build -tags=consul -o "${outdir}/agent-${suffix}" "${ROOT}/cmd/agent"
}

# Matrix: (os arch extra_env)
build_one linux amd64 "GOAMD64=v1"
build_one linux amd64 "GOAMD64=v3"
build_one linux arm64 ""
build_one linux arm "GOARM=7"
build_one linux 386 ""
build_one linux riscv64 ""
build_one linux s390x ""
build_one linux loong64 ""
build_one darwin amd64 ""
build_one darwin arm64 ""

echo "Preparing Git tag ${RELEASE_TAG}"
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  git tag -f "${RELEASE_TAG}" || true
  git push origin "refs/tags/${RELEASE_TAG}" || true
else
  echo "Warning: not in a git repo, skipping tag/push. Set GH_REPO to use gh."
fi

echo "Uploading assets to GitHub Release ${RELEASE_TAG}"
ASSETS=()
while IFS= read -r f; do
  ASSETS+=("$f")
done < <(find "${DIST}" -type f \( -name 'agent-*' -o -name 'controller-*' \))
if [ ${#ASSETS[@]} -eq 0 ]; then
  echo "No built binaries found in ${DIST}"
  exit 1
fi

if gh ${GH_REPO:+--repo "$GH_REPO"} release view "${RELEASE_TAG}" >/dev/null 2>&1; then
  echo "Release ${RELEASE_TAG} exists, deleting and recreating..."
  gh ${GH_REPO:+--repo "$GH_REPO"} release delete "${RELEASE_TAG}" -y
fi
gh ${GH_REPO:+--repo "$GH_REPO"} release create "${RELEASE_TAG}" "${ASSETS[@]}" -t "${RELEASE_TAG}" -n "peer-wan release ${RELEASE_TAG}"

echo "Done. Assets in ${DIST}"
