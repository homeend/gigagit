package worktree

import (
	"os"
	"path/filepath"
	"strings"
)

// Cross-environment path logic behind the worktree switch guards (TUI +
// web). A repo on a disk shared between WSL and Windows (/mnt/t/… vs T:\…)
// can hold worktrees whose recorded absolute paths only parse in the
// environment that created them. Everything here is pure: GOOS and stat are
// injected by the caller, so every branch is testable on any platform.
// (Moved from internal/tui/crossenv.go so internal/web can share it —
// archtest forbids web→tui.)

// SwitchVerdict classifies a switch target before any re-root teardown.
type SwitchVerdict int

const (
	SwitchOK          SwitchVerdict = iota // reachable as recorded — switch normally
	SwitchRepairable                       // reachable only under the other notation — offer git worktree repair
	SwitchUnreachable                      // not reachable at all — refuse
)

// TranslatePath converts a path between the WSL and Windows notations of the
// same disk location: on "windows", /mnt/<x>/rest → <X>:\rest; on "linux"
// (the WSL case), <X>:\rest or <X>:/rest → /mnt/<x>/rest. Any other input or
// GOOS is not translatable. Pure string work — it never stats.
func TranslatePath(goos, path string) (string, bool) {
	switch goos {
	case "windows":
		if len(path) >= 6 && strings.HasPrefix(path, "/mnt/") {
			d := path[5]
			if d < 'a' || d > 'z' {
				return "", false
			}
			drive := string(d - 'a' + 'A')
			rest := path[6:]
			if rest == "" {
				return drive + `:\`, true // never a drive-relative "<X>:"
			}
			if rest[0] != '/' {
				return "", false // /mnt/tt/… — a multi-letter mount, not a drive
			}
			return drive + ":" + strings.ReplaceAll(rest, "/", `\`), true
		}
	case "linux":
		if len(path) >= 2 && path[1] == ':' {
			d := path[0]
			switch {
			case d >= 'A' && d <= 'Z':
				d = d - 'A' + 'a'
			case d >= 'a' && d <= 'z':
			default:
				return "", false
			}
			rest := strings.ReplaceAll(path[2:], `\`, "/")
			if rest == "" {
				return "/mnt/" + string(d), true
			}
			if rest[0] != '/' {
				return "", false
			}
			return "/mnt/" + string(d) + rest, true
		}
	}
	return "", false
}

// NormalizeWorktreeLink rewrites <path>/.git when its gitdir pointer is in
// the OTHER environment's notation and the translated location exists on
// this one. `git worktree repair <path>` can heal a broken ADMIN record
// (reading a valid .git file) or a broken .git file (from a valid admin
// record) — but a worktree created by the other environment has BOTH
// records foreign, and repair then fixes neither (measured). Normalizing
// the .git side first turns that state into the admin-only breakage repair
// handles. Best-effort: any unexpected shape leaves the file untouched.
// Returns whether it rewrote. stat and goos are injected (testable on any
// platform); read/write use the real filesystem.
func NormalizeWorktreeLink(stat func(string) error, goos, path string) bool {
	link := filepath.Join(path, ".git")
	b, err := os.ReadFile(link)
	if err != nil {
		return false
	}
	content := strings.TrimSpace(string(b))
	const prefix = "gitdir:"
	if !strings.HasPrefix(content, prefix) {
		return false
	}
	target := strings.TrimSpace(content[len(prefix):])
	if target == "" || stat(target) == nil {
		return false // empty, or already reachable — nothing to normalize
	}
	tp, ok := TranslatePath(goos, target)
	if !ok || stat(tp) != nil {
		return false
	}
	return os.WriteFile(link, []byte("gitdir: "+tp+"\n"), 0o644) == nil
}

// CheckSwitchTarget stats path; failing that, stats its cross-environment
// translation. The stat IS the WSL detection: translating and statting
// /mnt/c/… on a non-WSL Linux simply fails → SwitchUnreachable, no
// osrelease probe needed. Returns the verdict plus the path to use — the
// input for SwitchOK, the translated path for SwitchRepairable.
func CheckSwitchTarget(stat func(string) error, goos, path string) (SwitchVerdict, string) {
	if stat(path) == nil {
		return SwitchOK, path
	}
	if tp, ok := TranslatePath(goos, path); ok && stat(tp) == nil {
		return SwitchRepairable, tp
	}
	return SwitchUnreachable, ""
}
