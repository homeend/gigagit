#!/usr/bin/env bash
# Build the gg executable for Linux and Windows into the project root.
#
#   ./build.sh            # build both: ./gg (linux) and ./gg.exe (windows)
#   ./build.sh linux      # build only ./gg
#   ./build.sh windows    # build only ./gg.exe
#   ./build.sh install    # install into GOBIN, version-stamped
#   ./build.sh web        # build ./gg-web-new.exe (windows) for run-win.cmd
#
# The web target exists because run-win.cmd cannot overwrite a running
# gg-web.exe — Windows locks it. It writes gg-web-new.exe instead, which
# run-win.cmd renames into place on the next launch. Run it in the worktree
# you want to serve; the exe lands beside this script.
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

# install puts gg on PATH via GOBIN, carrying the same -ldflags the build
# targets use. Plain `go install ./cmd/gg` works too, but without those flags
# buildinfo falls back to runtime/debug metadata and `gg version` reports a
# pseudo-version (v0.1.21-0.20260725085339-809b4259d45c+dirty) instead of the
# tag-relative one (v0.1.20-217-g809b4259).
#
# Replacing a copy that is currently running is fine: the Go toolchain writes
# a temp file and renames it into place, and renaming over a running
# executable is legal — only opening the existing file for writing is not. A
# live `gg mcp` server keeps serving from the old inode; the next one spawned
# picks up this build.
install() {
	local bin
	bin="$(go env GOBIN)"
	[ -n "${bin}" ] || bin="$(go env GOPATH)/bin"
	echo "installing ${bin}/gg (${ARCH}) ..."
	CGO_ENABLED=0 GOARCH="${ARCH}" \
		go install -trimpath -ldflags "${LDFLAGS}" "${PKG}"
}

target="${1:-linux}"
case "${target}" in
	linux)   build linux   ./gg ;;
	windows) build windows ./gg.exe ;;
	all)     build linux ./gg; build windows ./gg.exe ;;
	install) install ;;
	web)
		build windows ./gg-web-new.exe
		echo "wrote $(pwd)/gg-web-new.exe — run-win.cmd swaps it in on next launch"
		;;
	*) echo "usage: $0 [linux|windows|all|install|web]" >&2; exit 2 ;;
esac

echo "done: ${VERSION} (${COMMIT})"
