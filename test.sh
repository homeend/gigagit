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

# run_tests streams one line per package AS IT FINISHES (ok/FAIL/no-tests,
# with elapsed time and test count), so a long stage shows live progress
# instead of minutes of silence followed by one burst. A failing package
# dumps its full captured output right under its FAIL line. Verbose mode
# keeps go test's own raw -v stream. The pipeline's exit code is go test's
# (pipefail is set), so failures still stop the script.
run_tests() {
	if [[ -n "${VERBOSE}" ]]; then
		go test -timeout 30m ${RACE} ${VERBOSE} "$@"
		return
	fi
	go test -timeout 30m ${RACE} -json "$@" | awk '
	function pkgOf(line,   p) {
		if (match(line, /"Package":"[^"]*"/) == 0) return ""
		p = substr(line, RSTART + 11, RLENGTH - 12)
		sub(/^github\.com\/homeend\/gigagit\//, "", p)
		return p
	}
	{
		pkg = pkgOf($0)
		if (pkg == "") next
		isTest = ($0 ~ /"Test":"/)
		if ($0 ~ /"Action":"output"/) {
			# Buffer package output (decoded) so a FAIL can replay it.
			if (match($0, /"Output":"/)) {
				out = substr($0, RSTART + 10)
				sub(/"\}[[:space:]]*$/, "", out)
				gsub(/\\n/, "\n", out); gsub(/\\t/, "\t", out)
				gsub(/\\"/, "\"", out); gsub(/\\\\/, "\\", out)
				buf[pkg] = buf[pkg] out
			}
			next
		}
		if (isTest) {
			if ($0 ~ /"Action":"pass"/) tests[pkg]++
			next
		}
		# Package-level verdicts stream in completion order — the progress.
		if ($0 ~ /"Action":"pass"/) {
			el = ""
			if (match($0, /"Elapsed":[0-9.]+/)) el = substr($0, RSTART + 10, RLENGTH - 10) "s"
			if (buf[pkg] ~ /\(cached\)/) el = "(cached)"
			printf "ok   %-28s %8s  %d tests\n", pkg, el, tests[pkg]
			delete buf[pkg]; fflush()
		} else if ($0 ~ /"Action":"fail"/) {
			printf "FAIL %s\n", pkg
			printf "%s", buf[pkg]
			delete buf[pkg]; fflush()
		} else if ($0 ~ /"Action":"skip"/) {
			printf "--   %-28s (no test files)\n", pkg
			fflush()
		}
	}'
}

# Unit tests cover every package except the e2e harness; ./cmd/... and
# ./internal/... are the only other package roots in this module.
unit() {
	echo "== unit tests =="
	run_tests ./cmd/... ./internal/...
}

e2e() {
	echo "== e2e scenarios (last: full CLI→engine→git stack) =="
	run_tests ./e2e/
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
