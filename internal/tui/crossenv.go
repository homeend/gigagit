package tui

import "strings"

// Cross-environment path logic behind the worktree switch guard. A repo on a
// disk shared between WSL and Windows (/mnt/t/… vs T:\…) can hold worktrees
// whose recorded absolute paths only parse in the environment that created
// them. Everything here is pure: GOOS and stat are injected by the caller,
// so every branch is testable on any platform.

// switchVerdict classifies a switch target before any reRoot teardown.
type switchVerdict int

const (
	switchOK          switchVerdict = iota // reachable as recorded — switch normally
	switchRepairable                       // reachable only under the other notation — offer git worktree repair
	switchUnreachable                      // not reachable at all — refuse
)

// translatePath converts a path between the WSL and Windows notations of the
// same disk location: on "windows", /mnt/<x>/rest → <X>:\rest; on "linux"
// (the WSL case), <X>:\rest or <X>:/rest → /mnt/<x>/rest. Any other input or
// GOOS is not translatable. Pure string work — it never stats.
func translatePath(goos, path string) (string, bool) {
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

// checkSwitchTarget stats path; failing that, stats its cross-environment
// translation. The stat IS the WSL detection: translating and statting
// /mnt/c/… on a non-WSL Linux simply fails → switchUnreachable, no
// osrelease probe needed. Returns the verdict plus the path to use — the
// input for switchOK, the translated path for switchRepairable.
func checkSwitchTarget(stat func(string) error, goos, path string) (switchVerdict, string) {
	if stat(path) == nil {
		return switchOK, path
	}
	if tp, ok := translatePath(goos, path); ok && stat(tp) == nil {
		return switchRepairable, tp
	}
	return switchUnreachable, ""
}
