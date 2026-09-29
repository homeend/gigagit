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

# -count=1 turns go's test cache off. With the cache on, every test binary
# logs each file it stats/opens (the TUI suite logs ~700k entries, mostly
# PATH probes) and cmd/go re-checks every entry against the module root
# after the run — resolving the root's symlinks each time. With the checkout
# on a 9p mount (/mnt/<drive> under WSL) that phase alone ran for many
# minutes with no test process alive. The big packages rerun on every
# change anyway, so the cache buys little here.
NOCACHE="-count=1"

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
# prints, right under its FAIL line, the output of each test that FAILED
# (named, in failure order) and then its package-level output — a panic, a
# race report, a timeout — never the thousands of lines its passing tests
# wrote. Verbose mode keeps go test's own raw -v stream. The pipeline's exit
# code is go test's (pipefail is set), so failures still stop the script.
run_tests() {
	if [[ -n "${VERBOSE}" ]]; then
		go test -timeout 30m ${NOCACHE} ${RACE} ${VERBOSE} "$@"
		return
	fi
	go test -timeout 30m ${NOCACHE} ${RACE} -json "$@" | awk '
	function pkgOf(line,   p) {
		if (match(line, /"Package":"[^"]*"/) == 0) return ""
		p = substr(line, RSTART + 11, RLENGTH - 12)
		sub(/^github\.com\/homeend\/gigagit\//, "", p)
		return p
	}
	function testOf(line) {
		if (match(line, /"Test":"[^"]*"/) == 0) return ""
		return substr(line, RSTART + 8, RLENGTH - 9)
	}
	# outputOf decodes an output event'"'"'s text. Newer go test -json events
	# carry more fields after "Output" (e.g. "OutputType":"frame"), so the
	# value ends at the first unescaped quote, not at the closing brace.
	function outputOf(line,   out, i, c, s) {
		if (match(line, /"Output":"/) == 0) return ""
		out = substr(line, RSTART + 10)
		s = ""
		for (i = 1; i <= length(out); i++) {
			c = substr(out, i, 1)
			if (c == "\\") {
				c = substr(out, ++i, 1)
				if (c == "n") s = s "\n"
				else if (c == "t") s = s "\t"
				else if (c == "u") { s = s "?"; i += 4 } # \u00XX: control/HTML-escaped rune
				else s = s c
				continue
			}
			if (c == "\"") break
			s = s c
		}
		return s
	}
	{
		pkg = pkgOf($0)
		if (pkg == "") next
		test = testOf($0)
		if ($0 ~ /"Action":"output"/) {
			out = outputOf($0)
			if (out ~ /\(cached\)/) cached[pkg] = 1
			# Per test, so a FAIL replays only the failing tests; package
			# output (no Test) is its own buffer.
			if (test != "") tbuf[pkg, test] = tbuf[pkg, test] out
			else pbuf[pkg] = pbuf[pkg] out
			next
		}
		if (test != "") {
			if ($0 ~ /"Action":"pass"/) tests[pkg]++
			if ($0 ~ /"Action":"fail"/) failed[pkg] = failed[pkg] tbuf[pkg, test]
			if ($0 ~ /"Action":"(pass|fail|skip)"/) delete tbuf[pkg, test]
			next
		}
		# Package-level verdicts stream in completion order — the progress.
		if ($0 ~ /"Action":"pass"/) {
			el = ""
			if (match($0, /"Elapsed":[0-9.]+/)) el = substr($0, RSTART + 10, RLENGTH - 10) "s"
			if (cached[pkg]) el = "(cached)"
			printf "ok   %-28s %8s  %d tests\n", pkg, el, tests[pkg]
			delete pbuf[pkg]; fflush()
		} else if ($0 ~ /"Action":"fail"/) {
			printf "FAIL %s\n", pkg
			printf "%s", failed[pkg]
			# A test still running when the package died (a panic in
			# another goroutine, the -timeout) never got its own verdict:
			# its output is where the story is.
			for (k in tbuf) {
				split(k, kp, SUBSEP)
				if (kp[1] == pkg) { printf "%s", tbuf[k]; delete tbuf[k] }
			}
			printf "%s", pbuf[pkg]
			delete pbuf[pkg]; delete failed[pkg]; fflush()
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
