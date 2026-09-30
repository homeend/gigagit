#!/usr/bin/env bash
# Pack this repo plus everything the dev tools keep OUTSIDE it, keyed on its
# path, into one file — and unpack that file on another machine.
#
#   scripts/dev-bundle.sh pack [--no-transcripts] [-o <file>]
#   bash dev-bundle.sh unpack <file> [--to <dir>] [--canonical <path>] [--force]
#
# A bundle holds:
#   repo/             the whole checkout: .git (branches, stashes, hooks),
#                     worktrees, ignored files
#   claude-project/   ~/.claude/projects/<key>: memory + session transcripts
#                     (--no-transcripts keeps the memory only)
#   claude-entry.json this project's entry in ~/.claude.json (trust, MCP servers)
#   gg-state/         gg's per-repo stores (notes, shelves, bookmarks, …)
#   gg-global/        gg's config and global profiles/prefixes
#   dev-bundle.sh     this script, so the other machine needs nothing first:
#                       tar -xf <file> ./dev-bundle.sh && bash dev-bundle.sh unpack <file>
#
# Both tools key their state on the repo's absolute path (the "canonical"
# path, e.g. /work/gigagit). Unpack keeps that path by default, so nothing
# needs rewriting. --to puts the files somewhere else and tells you the one
# bind-mount line that makes the canonical path point there. --canonical
# re-keys everything to a different path instead.
#
# Not included: Claude plugins, global ~/.claude settings, the claude-mem
# database, Go/git themselves. Unpack never overwrites an existing file
# unless --force is given.
set -euo pipefail

die() { echo "dev-bundle: $*" >&2; exit 1; }

# Claude Code names a project folder after its cwd, every non-alphanumeric
# character turned into a dash.
claude_key() { printf %s "$1" | sed 's/[^A-Za-z0-9]/-/g'; }

# gg files per-repo state under sha256(<git common dir>)[:16].
gg_key() { printf %s "$1/.git" | sha256sum | cut -c1-16; }

re_escape() { printf %s "$1" | sed 's/[][\.*^$|]/\\&/g'; }

state_home() { echo "${XDG_STATE_HOME:-$HOME/.local/state}/gg"; }
config_home() { echo "${XDG_CONFIG_HOME:-$HOME/.config}/gg"; }

do_pack() {
	local out="" transcripts=1
	while [[ $# -gt 0 ]]; do
		case "$1" in
			--no-transcripts) transcripts="" ;;
			-o) shift; out="$1" ;;
			*) die "pack: unknown argument $1" ;;
		esac
		shift
	done
	local top ckey gkey cdir state conf stage
	top="$(git rev-parse --show-toplevel)"
	[[ "$(git rev-parse --path-format=absolute --git-common-dir)" == "${top}/.git" ]] \
		|| die "run pack from the MAIN checkout, not a worktree"
	ckey="$(claude_key "${top}")"
	gkey="$(gg_key "${top}")"
	cdir="$HOME/.claude/projects/${ckey}"
	state="$(state_home)"
	conf="$(config_home)"
	[[ -n "${out}" ]] || out="$PWD/$(basename "${top}")-dev-$(date +%Y%m%d-%H%M).tar.zst"
	out="$(realpath -m "${out}")"

	stage="$(mktemp -d)"
	trap 'rm -rf "${stage}"' RETURN
	cp "${top}/scripts/dev-bundle.sh" "${stage}/dev-bundle.sh"
	{
		echo "canonical=${top}"
		echo "claude_key=${ckey}"
		echo "gg_key=${gkey}"
		echo "created=$(date -Is)"
		echo "host=$(hostname)"
		echo "head=$(git rev-parse HEAD)"
	} >"${stage}/manifest"
	python3 - "${top}" "${stage}/claude-entry.json" <<'EOF'
import json, os, sys
top, out = sys.argv[1], sys.argv[2]
try:
    entry = json.load(open(os.path.expanduser("~/.claude.json")))["projects"][top]
except (OSError, KeyError, ValueError):
    entry = {}
entry = {k: v for k, v in entry.items() if not k.startswith("last")}
json.dump(entry, open(out, "w"), indent=2)
EOF

	# One tar run: every source is named relative to / and renamed on the
	# way in (flags=rh: member and hard-link names, never symlink targets).
	local -a items=() xf=()
	add() { # <absolute source> <name in the bundle>
		[[ -e "$1" ]] || return 0
		items+=("${1#/}")
		xf+=("--transform=flags=rh;s|^$(re_escape "${1#/}")|$2|")
	}
	add "${top}" repo
	if [[ -n "${transcripts}" ]]; then
		add "${cdir}" claude-project
	else
		add "${cdir}/memory" claude-project/memory
	fi
	local store
	for store in "${state}"/*/; do
		store="$(basename "${store}")"
		add "${state}/${store}/${gkey}" "gg-state/${store}"
		add "${state}/${store}/global" "gg-global/state/${store}"
	done
	add "${conf}" gg-global/config

	local -a comp=(--zstd)
	command -v zstd >/dev/null || { comp=(-z); out="${out%.zst}.gz"; }
	tar "${comp[@]}" -cf "${out}" \
		--exclude='windows-mirror.lock' \
		-C "${stage}" . \
		"${xf[@]}" -C / "${items[@]}"
	echo "packed ${top} → ${out} ($(du -h "${out}" | cut -f1))"
	echo "on the other machine:"
	echo "  tar -xf $(basename "${out}") ./dev-bundle.sh && bash dev-bundle.sh unpack $(basename "${out}")"
}

do_unpack() {
	local file="" to="" canonical="" force=""
	while [[ $# -gt 0 ]]; do
		case "$1" in
			--to) shift; to="$1" ;;
			--canonical) shift; canonical="$1" ;;
			--force) force=1 ;;
			*) file="$1" ;;
		esac
		shift
	done
	[[ -f "${file}" ]] || die "usage: unpack <file> [--to <dir>] [--canonical <path>] [--force]"
	file="$(realpath "${file}")"

	local was
	was="$(tar -xf "${file}" -O ./manifest | sed -n 's/^canonical=//p')"
	[[ -n "${was}" ]] || die "${file} has no manifest — not a dev bundle"
	[[ -n "${canonical}" ]] || canonical="${was}"
	[[ -n "${to}" ]] || to="${canonical}"
	local -a keep=(--skip-old-files)
	[[ -z "${force}" ]] || keep=(--overwrite)
	has() { tar -tf "${file}" "$1" >/dev/null 2>&1; }

	# 1. the repo
	if [[ -d "${to}" && -n "$(ls -A "${to}" 2>/dev/null)" && -z "${force}" ]]; then
		die "${to} is not empty (pass --force to unpack over it)"
	fi
	mkdir -p "${to}" 2>/dev/null || die "cannot create ${to} — create it first (sudo mkdir -p ${to} && sudo chown \$USER ${to}) or pass --to <dir>"
	tar -xf "${file}" -C "${to}" --strip-components=1 "${keep[@]}" repo
	git -C "${to}" worktree repair >/dev/null 2>&1 || true
	echo "repo            → ${to}"

	# 2. Claude: project folder (memory + transcripts) and the config entry
	local cdir="$HOME/.claude/projects/$(claude_key "${canonical}")"
	if has claude-project; then
		mkdir -p "${cdir}"
		tar -xf "${file}" -C "${cdir}" --strip-components=1 "${keep[@]}" claude-project
		if [[ "${canonical}" != "${was}" && -d "${cdir}/memory" ]]; then
			WAS="${was}" NOW="${canonical}" perl -pi -e 's{\Q$ENV{WAS}\E(?![.\w-])}{$ENV{NOW}}g' "${cdir}"/memory/*.md
		fi
		echo "claude project  → ${cdir}"
	fi
	tar -xf "${file}" -O ./claude-entry.json | python3 -c '
import json, os, sys
canonical, force = sys.argv[1], sys.argv[2] == "1"
entry = json.load(sys.stdin)
path = os.path.expanduser("~/.claude.json")
try:
    conf = json.load(open(path))
except (OSError, ValueError):
    conf = {}
projects = conf.setdefault("projects", {})
if entry and (force or canonical not in projects):
    projects[canonical] = entry
    tmp = path + ".tmp-bundle"
    json.dump(conf, open(tmp, "w"), indent=2)
    os.replace(tmp, path)
    print("claude settings → ~/.claude.json [" + canonical + "]")
else:
    print("claude settings : kept the existing entry")
' "${canonical}" "${force:-0}"

	# 3. gg: per-repo stores under the new path's key, globals only if absent
	local state conf gkey store
	state="$(state_home)"
	conf="$(config_home)"
	gkey="$(gg_key "${canonical}")"
	for store in $(tar -tf "${file}" | sed -n 's|^gg-state/\([^/]*\)/$|\1|p'); do
		mkdir -p "${state}/${store}/${gkey}"
		tar -xf "${file}" -C "${state}/${store}/${gkey}" --strip-components=2 "${keep[@]}" "gg-state/${store}"
	done
	echo "gg state        → ${state}/*/${gkey}"
	for store in $(tar -tf "${file}" | sed -n 's|^gg-global/state/\([^/]*\)/$|\1|p'); do
		mkdir -p "${state}/${store}/global"
		tar -xf "${file}" -C "${state}/${store}/global" --strip-components=3 --skip-old-files "gg-global/state/${store}"
	done
	if has gg-global/config; then
		mkdir -p "${conf}"
		tar -xf "${file}" -C "${conf}" --strip-components=2 --skip-old-files gg-global/config
	fi

	# 4. what is left for a human
	echo
	if [[ ! "${to}" -ef "${canonical}" ]]; then
		echo "The tools expect the repo at ${canonical}. Make that path point at ${to}:"
		echo "  sudo mkdir -p ${canonical} && echo '$(realpath "${to}") ${canonical} none bind 0 0' | sudo tee -a /etc/fstab && sudo systemctl daemon-reload && sudo mount ${canonical}"
	fi
	local mirror
	mirror="$(git -C "${to}" config --get mirror.windowsPath || true)"
	if [[ -n "${mirror}" && ! -d "${mirror}" ]]; then
		echo "The Windows mirror (${mirror}) does not exist here. Drop it with"
		echo "  (cd ${canonical} && scripts/windows-mirror.sh uninstall)   or re-run its install with a new path."
	fi
	echo "Then: install Go + git, run ./build.sh install in ${canonical}, and start Claude and gg from ${canonical}."
}

case "${1:-}" in
	pack) shift; do_pack "$@" ;;
	unpack) shift; do_unpack "$@" ;;
	*) die "usage: $0 pack [--no-transcripts] [-o <file>] | unpack <file> [--to <dir>] [--canonical <path>] [--force]" ;;
esac
