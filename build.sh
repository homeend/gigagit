#!/usr/bin/env bash
# Build the gg executable for Linux and Windows into the project root.
#
#   ./build.sh            # build both: ./gg (linux) and ./gg.exe (windows)
#   ./build.sh linux      # build only ./gg
#   ./build.sh windows    # build only ./gg.exe
#
# GOARCH may be overridden (default: amd64), e.g. GOARCH=arm64 ./build.sh linux
set -euo pipefail

# Run from the project root (this script's directory) regardless of CWD.
cd "$(dirname "$0")"

PKG="./cmd/gg"
ARCH="${GOARCH:-amd64}"

# Version metadata injected into internal/buildinfo via -ldflags.
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
LDFLAGS="-s -w \
-X github.com/homeend/gigagit/internal/buildinfo.Version=${VERSION} \
-X github.com/homeend/gigagit/internal/buildinfo.Commit=${COMMIT}"

build() {
	local goos="$1" out="$2"
	echo "building ${out} (${goos}/${ARCH}) ..."
	CGO_ENABLED=0 GOOS="${goos}" GOARCH="${ARCH}" \
		go build -trimpath -ldflags "${LDFLAGS}" -o "${out}" "${PKG}"
}

target="${1:-linux}"
case "${target}" in
	linux)   build linux   ./gg ;;
	windows) build windows ./gg.exe ;;
	all)     build linux ./gg; build windows ./gg.exe ;;
	*) echo "usage: $0 [linux|windows|all]" >&2; exit 2 ;;
esac

echo "done: ${VERSION} (${COMMIT})"
