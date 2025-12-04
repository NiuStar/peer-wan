GH_REPO=NiuStar/peer-wan \
RELEASE_TAG=${RELEASE_TAG:-v$(TZ=Asia/Shanghai date +%Y-%m-%d-%H-%M)} \
scripts/build-release.sh
