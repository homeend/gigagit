#!/usr/bin/env bash
# Pack this repo plus everything the dev tools keep OUTSIDE it into one file,
# and rebuild the whole working setup from that file on another machine.
#
#   scripts/dev-bundle.sh pack [--no-transcripts] [--no-machine] [--with-claude-mem] [-o <file>]
#   bash dev-bundle.sh unpack <file> [--to <dir>] [--canonical <path>] [--install-tools] [--force] [--dry-run]
#   bash dev-bundle.sh tools <file> [--install] [--dry-run]     # only the toolchain step
#   bash dev-bundle.sh plugins <file> [--dry-run]               # only the Claude plugin step
#
# A bundle holds:
#   repo/             the whole checkout: .git (branches, stashes, hooks),
#                     worktrees, ignored files
#   claude-project/   ~/.claude/projects/<key>: memory + session transcripts
#                     (--no-transcripts keeps the memory only)
#   claude-entry.json this project's entry in ~/.claude.json (trust, MCP servers)
#   gg-state/         gg's per-repo stores (notes, shelves, bookmarks, …)
#   gg-global/        gg's config and global profiles/prefixes
#   machine/          the Claude setup of this machine (skipped by --no-machine):
#                       claude/       CLAUDE.md, RTK.md, settings, status line,
#                                     skills, agents, commands, hooks
#                       plugins.tsv   marketplaces + installed plugins — they are
#                                     REINSTALLED on the other side, not copied
#                                     (their caches hold machine-built binaries)
#                       tools.txt     versions of go, git, node, claude, rtk, …
#   claude-mem/       the claude-mem database (only with --with-claude-mem;
#                     its vector index and logs stay behind)
#   dev-bundle.sh     this script, so the other machine needs nothing first:
#                       tar -xf <file> ./dev-bundle.sh && bash dev-bundle.sh unpack <file> --install-tools
#
# Both tools key their state on the repo's absolute path (the "canonical"
# path, e.g. /work/gigagit). Unpack keeps that path by default, so nothing
# needs rewriting. --to puts the files somewhere else and tells you the one
# bind-mount line that makes the canonical path point there. --canonical
# re-keys everything to a different path instead.
#
# Never packed: logins. ~/.claude/.credentials.json and the rest of
# ~/.claude.json stay behind — log in again on the other machine.
# Unpack never overwrites an existing file unless --force is given.
set -euo pipefail

DRY=""

die() { echo "dev-bundle: $*" >&2; exit 1; }

# run echoes a command and, unless --dry-run, executes it.
run() {
	echo "  \$ $*"
	[[ -n "${DRY}" ]] || "$@"
}

# Claude Code names a project folder after its cwd, every non-alphanumeric
# character turned into a dash.
claude_key() { printf %s "$1" | sed 's/[^A-Za-z0-9]/-/g'; }

# gg files per-repo state under sha256(<git common dir>)[:16].
gg_key() { printf %s "$1/.git" | sha256sum | cut -c1-16; }

re_escape() { printf %s "$1" | sed 's/[][\.*^$|]/\\&/g'; }

state_home() { echo "${XDG_STATE_HOME:-$HOME/.local/state}/gg"; }
config_home() { echo "${XDG_CONFIG_HOME:-$HOME/.config}/gg"; }

manifest() { tar -xf "$1" -O ./manifest | sed -n "s/^$2=//p"; }
has() { tar -tf "$1" "$2" >/dev/null 2>&1; }

# first version-looking token of `<cmd> <flag>`, or nothing
ver() { "$@" 2>/dev/null | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -1 || true; }

tool_versions() {
	echo "git=$(ver git --version)"
	echo "go=$(ver go version)"
	echo "node=$(ver node --version)"
	echo "bun=$(ver bun --version)"
	echo "claude=$(ver claude --version)"
	echo "rtk=$(ver rtk --version)"
	echo "zstd=$(ver zstd --version)"
	echo "tmux=$(ver tmux -V)"
}

do_pack() {
	local out="" transcripts=1 machine=1 mem=""
	while [[ $# -gt 0 ]]; do
		case "$1" in
			--no-transcripts) transcripts="" ;;
			--no-machine) machine="" ;;
			--with-claude-mem) mem=1 ;;
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
		echo "home=${HOME}"
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

	if [[ -n "${machine}" ]]; then
		mkdir -p "${stage}/machine"
		tool_versions >"${stage}/machine/tools.txt"
		python3 - "${stage}/machine/plugins.tsv" <<'EOF'
import json, os, sys
home = os.path.expanduser("~/.claude/")
def load(p):
    try:
        return json.load(open(home + p))
    except (OSError, ValueError):
        return {}
enabled = load("settings.json").get("enabledPlugins", {})
with open(sys.argv[1], "w") as out:
    for name, m in sorted(load("plugins/known_marketplaces.json").items()):
        src = m.get("source", {})
        out.write("marketplace\t%s\t%s\t%s\n" % (name, src.get("source", ""), src.get("repo") or src.get("path") or src.get("url") or ""))
    for pid, installs in sorted(load("plugins/installed_plugins.json").get("plugins", {}).items()):
        for inst in installs:
            scope = inst.get("scope", "user")
            on = "on" if scope != "user" or enabled.get(pid, True) else "off"
            out.write("plugin\t%s\t%s\t%s\n" % (pid, scope, on))
EOF
		local f
		for f in CLAUDE.md RTK.md settings.json settings.local.json statusline.sh keybindings.json \
			package.json gsd-file-manifest.json gsd-install-state.json skills agents commands hooks; do
			add "$HOME/.claude/${f}" "machine/claude/${f}"
		done
		# a "directory" marketplace lives on this disk; github ones are re-fetched
		while IFS=$'\t' read -r kind name src loc; do
			[[ "${kind}" == marketplace && "${src}" == directory ]] || continue
			add "${loc}" "machine/claude/plugins/marketplaces/${name}"
		done <"${stage}/machine/plugins.tsv"
	fi

	if [[ -n "${mem}" && -f "$HOME/.claude-mem/claude-mem.db" ]]; then
		mkdir -p "${stage}/claude-mem"
		# a consistent copy even while the worker has the database open
		if command -v sqlite3 >/dev/null; then
			sqlite3 "$HOME/.claude-mem/claude-mem.db" ".backup '${stage}/claude-mem/claude-mem.db'"
		else
			cp "$HOME/.claude-mem"/claude-mem.db* "${stage}/claude-mem/"
		fi
		cp "$HOME/.claude-mem/settings.json" "${stage}/claude-mem/" 2>/dev/null || true
	fi

	local -a comp=(--zstd)
	command -v zstd >/dev/null || { comp=(-z); out="${out%.zst}.gz"; }
	tar "${comp[@]}" -cf "${out}" \
		--exclude='windows-mirror.lock' --exclude='.claude/skills/synced' \
		-C "${stage}" . \
		"${xf[@]}" -C / "${items[@]}"
	echo "packed ${top} → ${out} ($(du -h "${out}" | cut -f1))"
	echo "on the other machine:"
	echo "  tar -xf $(basename "${out}") ./dev-bundle.sh && bash dev-bundle.sh unpack $(basename "${out}") --install-tools"
}

# do_tools compares the packed tool versions with this machine and, with
# --install, installs what is missing (Debian/Ubuntu; sudo asks for itself).
do_tools() { # <file> [install]
	local file="$1" install="${2:-}" name want have missing=""
	has "${file}" ./machine/tools.txt || { echo "tools           : none recorded in this bundle"; return 0; }
	echo "tools (packed → here):"
	while IFS='=' read -r name want; do
		[[ -n "${want}" ]] || continue
		have="$(tool_versions | sed -n "s/^${name}=//p")"
		printf '  %-7s %-10s %s\n' "${name}" "${want}" "${have:-MISSING}"
		[[ -n "${have}" ]] || missing+=" ${name}"
	done < <(tar -xf "${file}" -O ./machine/tools.txt)
	[[ -n "${missing}" ]] || { echo "  all present"; return 0; }
	if [[ -z "${install}" ]]; then
		echo "  missing:${missing} — rerun with --install-tools (or: bash dev-bundle.sh tools <file> --install)"
		return 0
	fi
	command -v apt-get >/dev/null || die "automatic install knows apt only; install by hand:${missing}"
	local want_go want_node arch
	want_go="$(tar -xf "${file}" -O ./machine/tools.txt | sed -n 's/^go=//p')"
	want_node="$(tar -xf "${file}" -O ./machine/tools.txt | sed -n 's/^node=//p')"
	arch="$(dpkg --print-architecture)"
	for name in ${missing}; do
		case "${name}" in
			git)
				# Ubuntu's own git is too old for relative worktree paths (needs 2.48)
				run sudo add-apt-repository -y ppa:git-core/ppa
				run sudo apt-get update
				run sudo apt-get install -y git ;;
			zstd|tmux) run sudo apt-get install -y "${name}" ;;
			go)
				run bash -c "curl -fsSL https://go.dev/dl/go${want_go}.linux-${arch}.tar.gz | sudo tar -C /usr/local -xz"
				grep -q '/usr/local/go/bin' "$HOME/.profile" 2>/dev/null \
					|| run bash -c "echo 'export PATH=\$PATH:/usr/local/go/bin:\$HOME/go/bin' >> \$HOME/.profile"
				export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin" ;;
			node)
				run bash -c "curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.1/install.sh | bash"
				run bash -c "source \$HOME/.nvm/nvm.sh && nvm install ${want_node%%.*}"
				# this process never sourced nvm: put the new node on PATH by hand
				export PATH="$(ls -d "$HOME"/.nvm/versions/node/v*/bin 2>/dev/null | tail -1):$PATH" ;;
			bun)
				run bash -c "curl -fsSL https://bun.sh/install | bash"
				export PATH="$HOME/.bun/bin:$PATH" ;;
			claude)
				run bash -c "curl -fsSL https://claude.ai/install.sh | bash"
				export PATH="$HOME/.local/bin:$PATH" ;;
			rtk) echo "  rtk: install it the way this machine did (brew install rtk) — not automated" ;;
		esac
	done
}

# do_plugins re-adds the marketplaces and reinstalls the plugins recorded at
# pack time. Reinstalling (not copying) gets binaries built for THIS machine.
do_plugins() { # <file> <canonical> <old home>
	local file="$1" canonical="$2" was_home="$3" kind a b c
	has "${file}" ./machine/plugins.tsv || return 0
	if ! command -v claude >/dev/null && [[ -z "${DRY}" ]]; then
		echo "plugins         : claude is not installed yet — afterwards run: bash dev-bundle.sh plugins ${file}"
		return 0
	fi
	echo "plugins:"
	while IFS=$'\t' read -r kind a b c; do
		case "${kind}" in
			marketplace)
				[[ "${b}" != directory ]] || c="${c/#${was_home}/${HOME}}"
				run claude plugin marketplace add "${c}" </dev/null || true ;;
			plugin)
				if [[ "${b}" == user ]]; then
					run claude plugin install "${a}" --scope user </dev/null || true
					[[ "${c}" == on ]] || run claude plugin disable "${a}" </dev/null || true
				elif [[ -d "${canonical}" ]]; then
					# project/local scope is declared by the repo's own .claude/settings
					(cd "${canonical}" && run claude plugin install "${a}" --scope "${b}" </dev/null) || true
				fi ;;
		esac
	done < <(tar -xf "${file}" -O ./machine/plugins.tsv)
}

do_unpack() {
	local file="" to="" canonical="" force="" tools=""
	while [[ $# -gt 0 ]]; do
		case "$1" in
			--to) shift; to="$1" ;;
			--canonical) shift; canonical="$1" ;;
			--force) force=1 ;;
			--install-tools) tools=install ;;
			--dry-run) DRY=1 ;;
			*) file="$1" ;;
		esac
		shift
	done
	[[ -f "${file}" ]] || die "usage: unpack <file> [--to <dir>] [--canonical <path>] [--install-tools] [--force] [--dry-run]"
	file="$(realpath "${file}")"

	local was was_home
	was="$(manifest "${file}" canonical)"
	was_home="$(manifest "${file}" home)"
	[[ -n "${was}" ]] || die "${file} has no manifest — not a dev bundle"
	[[ -n "${canonical}" ]] || canonical="${was}"
	[[ -n "${to}" ]] || to="${canonical}"
	local -a keep=(--skip-old-files)
	[[ -z "${force}" ]] || keep=(--overwrite)

	# 0. toolchain first, so git/claude exist for the later steps
	do_tools "${file}" "${tools}"
	if [[ -n "${DRY}" ]]; then
		do_plugins "${file}" "${canonical}" "${was_home}"
		echo "(dry run: nothing was unpacked)"
		return 0
	fi

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
	if has "${file}" claude-project; then
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
	if has "${file}" gg-global/config; then
		mkdir -p "${conf}"
		tar -xf "${file}" -C "${conf}" --strip-components=2 --skip-old-files gg-global/config
	fi

	# 4. the machine's Claude setup: config files, then the plugins
	if has "${file}" machine/claude; then
		local fresh=""
		[[ -e "$HOME/.claude/settings.json" && -z "${force}" ]] || fresh=1
		mkdir -p "$HOME/.claude"
		tar -xf "${file}" -C "$HOME/.claude" --strip-components=2 "${keep[@]}" machine/claude
		# settings.json names hook and status-line scripts by absolute path
		if [[ -n "${fresh}" && -n "${was_home}" && "${was_home}" != "${HOME}" ]]; then
			WAS="${was_home}" NOW="${HOME}" perl -pi -e 's{\Q$ENV{WAS}\E}{$ENV{NOW}}g' "$HOME/.claude/settings.json"
		fi
		if [[ -n "${fresh}" ]]; then
			echo "claude setup    → ~/.claude (settings, CLAUDE.md, skills, agents, hooks)"
		else
			echo "claude setup    → ~/.claude (your existing settings.json was kept; new files only)"
		fi
	fi
	if has "${file}" ./claude-mem/claude-mem.db; then
		mkdir -p "$HOME/.claude-mem"
		tar -xf "${file}" -C "$HOME/.claude-mem" --strip-components=2 "${keep[@]}" ./claude-mem
		echo "claude-mem      → ~/.claude-mem (database only; its search index rebuilds or stays empty)"
	fi
	do_plugins "${file}" "${canonical}" "${was_home}"

	# 5. gg itself
	if command -v go >/dev/null && [[ -x "${to}/build.sh" ]]; then
		(cd "${to}" && ./build.sh install >/dev/null 2>&1) && echo "gg              → built and installed from ${to}" \
			|| echo "gg              : ./build.sh install failed — run it by hand in ${canonical}"
	fi

	# 6. what is left for a human
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
	echo "Then log in (run claude once) and start Claude and gg from ${canonical}."
}

case "${1:-}" in
	pack) shift; do_pack "$@" ;;
	unpack) shift; do_unpack "$@" ;;
	tools)
		shift; f=""; inst=""
		for a in "$@"; do case "$a" in --install) inst=install ;; --dry-run) DRY=1 ;; *) f="$a" ;; esac; done
		[[ -f "${f}" ]] || die "usage: tools <file> [--install] [--dry-run]"
		do_tools "$(realpath "${f}")" "${inst}" ;;
	plugins)
		shift; f=""
		for a in "$@"; do case "$a" in --dry-run) DRY=1 ;; *) f="$a" ;; esac; done
		[[ -f "${f}" ]] || die "usage: plugins <file> [--dry-run]"
		f="$(realpath "${f}")"
		do_plugins "${f}" "$(manifest "${f}" canonical)" "$(manifest "${f}" home)" ;;
	*) die "usage: $0 pack [...] | unpack <file> [...] | tools <file> [--install] | plugins <file>   (see the header of this file)" ;;
esac
