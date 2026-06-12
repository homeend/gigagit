#!/usr/bin/env bash
# Run gigagit's test suite in stages: quality gates, unit tests, then the
# e2e scenario suite LAST (it exercises the full CLI→engine→git stack and
# only makes sense once everything else is green).
#
#   ./test.sh            # gates + unit + e2e
#   ./test.sh unit       # unit tests only (./cmd/... ./internal/...)
#   ./test.sh e2e        # e2e scenarios only (./e2e)
#   ./test.sh race       # gates + unit + e2e, all with -race (pre-merge gate)
#
# Append -v to any form for verbose output — e2e scenarios then report what
# each one verified and every gg command's exit, e.g. ./test.sh e2e -v
set -euo pipefail

# Run from the project root (this script's directory) regardless of CWD.
cd "$(dirname "$0")"

RACE=""
VERBOSE=""

gates() {
	echo "== quality gates: go vet + gofmt =="
	go vet ./...
	local unformatted
	unformatted="$(gofmt -l internal/ cmd/ e2e/)"
	if [[ -n "${unformatted}" ]]; then
		echo "gofmt: files need formatting:" >&2
		echo "${unformatted}" >&2
		exit 1
	fi
}

# Unit tests cover every package except the e2e harness; ./cmd/... and
# ./internal/... are the only other package roots in this module.
unit() {
	echo "== unit tests =="
	go test ${RACE} ${VERBOSE} ./cmd/... ./internal/...
}

e2e() {
	echo "== e2e scenarios (last: full CLI→engine→git stack) =="
	go test ${RACE} ${VERBOSE} ./e2e/
}

target="${1:-all}"
if [[ "${target}" == "-v" ]]; then
	target="all"
	VERBOSE="-v"
elif [[ "${2:-}" == "-v" ]]; then
	VERBOSE="-v"
fi
case "${target}" in
	unit) unit ;;
	e2e)  e2e ;;
	race) RACE="-race"; gates; unit; e2e ;;
	all)  gates; unit; e2e ;;
	*) echo "usage: $0 [unit|e2e|race] [-v]" >&2; exit 2 ;;
esac

echo "all green"
