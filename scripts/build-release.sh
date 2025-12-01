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
  env CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" ${extra_env} go build -tags=consul -o "${outdir}/controller" "${ROOT}/cmd/controller"
  env CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" ${extra_env} go build -tags=consul -o "${outdir}/agent" "${ROOT}/cmd/agent"
  tar -C "${outdir}" -czf "${DIST}/peer-wan-${suffix}.tar.gz" controller agent
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

echo "Uploading assets to GitHub Release ${RELEASE_TAG}"
ASSETS=("${DIST}"/peer-wan-*.tar.gz)

if gh release view "${RELEASE_TAG}" >/dev/null 2>&1; then
  gh release upload "${RELEASE_TAG}" "${ASSETS[@]}"
else
  gh release create "${RELEASE_TAG}" "${ASSETS[@]}" -t "${RELEASE_TAG}" -n "peer-wan release ${RELEASE_TAG}"
fi

echo "Done. Assets in ${DIST}"
