#!/usr/bin/env bash
# Keep a build-only copy of `main` on the Windows filesystem, so Windows can
# build gg.exe from the WSL repo without developing over 9p.
#
#   scripts/windows-mirror.sh install /mnt/t/others/gigagit   # once per repo
#   scripts/windows-mirror.sh sync                            # by hand
#   scripts/windows-mirror.sh uninstall
#
# install clones this repo to the given path (or adopts an existing clone
# with --adopt), records the path in `git config mirror.windowsPath`, and
# installs a reference-transaction hook. The hook fires whenever
# refs/heads/main moves — commit, merge, rebase, fast-forward pull,
# update-ref, any gg op — and syncs in the background, so the commit never
# waits on 9p.
#
# A sync fetches main into the mirror and force-checks it out. Tracked files
# follow main exactly; untracked and ignored files (gg.exe, build output)
# are never touched. Do not edit in the mirror: tracked edits there are
# overwritten. The log is <git-common-dir>/windows-mirror.log.
set -euo pipefail

HOOK_MARK="# windows-mirror hook"

die() { echo "windows-mirror: $*" >&2; exit 1; }

# A hook runs with GIT_DIR & co. pointing at THIS repo; git -C <mirror> must
# not inherit them.
clean_git_env() {
	# shellcheck disable=SC2046
	unset $(git rev-parse --local-env-vars)
}

common_dir() { git rev-parse --path-format=absolute --git-common-dir; }

do_sync() {
	local src dst
	src="$(common_dir)"
	dst="$(git config --get mirror.windowsPath || true)"
	[[ -n "${dst}" ]] || die "no mirror.windowsPath configured"
	clean_git_env
	# One sync at a time; a queued one fetches whatever main is by then.
	exec 9>"${src}/windows-mirror.lock"
	flock 9
	echo "== $(date '+%F %T') sync $(git -C "${src}" rev-parse --short main) → ${dst}"
	git -C "${dst}" fetch -q "${src}" "+refs/heads/main:refs/remotes/wsl/main"
	git -C "${dst}" checkout -q -f -B main refs/remotes/wsl/main
	echo "   done"
}

do_install() {
	local dst="" adopt=""
	for a in "$@"; do
		case "$a" in
			--adopt) adopt=1 ;;
			*) dst="$a" ;;
		esac
	done
	[[ -n "${dst}" ]] || die "usage: install <windows-path> [--adopt]"
	local src top
	src="$(common_dir)"
	top="$(git rev-parse --show-toplevel)"
	if [[ -e "${dst}" ]]; then
		[[ -n "${adopt}" ]] || die "${dst} exists; pass --adopt to reuse it (its tracked files will follow main, its local edits are lost)"
		git -C "${dst}" rev-parse --git-dir >/dev/null 2>&1 || die "${dst} is not a git repository"
	else
		git clone -q --no-checkout "${src}" "${dst}"
	fi
	# Windows git must see the files exactly as WSL git wrote them.
	git -C "${dst}" config core.autocrlf false
	git -C "${dst}" config core.fileMode false
	git config mirror.windowsPath "$(cd "${dst}" && pwd)"

	local hook="${src}/hooks/reference-transaction"
	if [[ -e "${hook}" ]] && ! grep -q "${HOOK_MARK}" "${hook}"; then
		die "${hook} already exists and is not ours; merge by hand"
	fi
	mkdir -p "${src}/hooks"
	cp "${top}/scripts/windows-mirror.sh" "${src}/hooks/windows-mirror.sh"
	chmod +x "${src}/hooks/windows-mirror.sh"
	cat >"${hook}" <<EOF
#!/usr/bin/env bash
${HOOK_MARK} (scripts/windows-mirror.sh install)
[ "\$1" = committed ] || exit 0
grep -q ' refs/heads/main\$' || exit 0
src="\$(git rev-parse --path-format=absolute --git-common-dir)"
setsid nohup "\${src}/hooks/windows-mirror.sh" sync </dev/null >>"\${src}/windows-mirror.log" 2>&1 &
exit 0
EOF
	chmod +x "${hook}"
	echo "installed: main → $(git config --get mirror.windowsPath)"
	do_sync
}

do_uninstall() {
	local src hook
	src="$(common_dir)"
	hook="${src}/hooks/reference-transaction"
	if [[ -e "${hook}" ]] && grep -q "${HOOK_MARK}" "${hook}"; then
		rm -f "${hook}"
	fi
	rm -f "${src}/hooks/windows-mirror.sh"
	git config --unset mirror.windowsPath || true
	echo "uninstalled (the mirror folder itself is left in place)"
}

case "${1:-}" in
	sync) do_sync ;;
	install) shift; do_install "$@" ;;
	uninstall) do_uninstall ;;
	*) die "usage: $0 install <windows-path> [--adopt] | sync | uninstall" ;;
esac
