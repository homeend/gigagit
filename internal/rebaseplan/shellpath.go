package rebaseplan

import "strings"

// ShellPath quotes a path for the shell GIT runs, which is NOT the platform
// shell: on Windows git executes GIT_SEQUENCE_EDITOR and every `exec` todo line
// through its own bundled POSIX sh, not cmd.exe. Two things follow.
//
// First, the quoting rules are POSIX everywhere — single quotes, with the
// `'\”` dance for an embedded quote — so cmd.exe's double-quote convention
// (internal/template's quoteArgFor, which quotes for the user's own shell)
// would be wrong here.
//
// Second, a Windows path's backslashes must become forward slashes BEFORE
// quoting. Unquoted they were eaten as escapes — `t:\others\gg.exe` reached sh
// as `t:othersgg.exe`, reported as "command not found" — and single-quoting
// them instead would hand sh a literal backslash path it will not resolve.
// Both sh and CreateProcess accept `t:/others/gg.exe`.
//
// The conversion is Windows-only on purpose: a backslash is a legal character
// in a POSIX filename, so rewriting one there would break a real path.
func ShellPath(p, goos string) string {
	if goos == "windows" {
		p = strings.ReplaceAll(p, `\`, "/")
	}
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}

// SequenceEditor builds the GIT_SEQUENCE_EDITOR value that re-invokes gg as
// the rebase todo editor. git appends the todo file as a further argument.
func SequenceEditor(ggBin, planPath, goos string) string {
	return ShellPath(ggBin, goos) + " __rebase-seq " + ShellPath(planPath, goos)
}
