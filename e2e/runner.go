package e2e

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/cli"
)

// Runner executes one gg command against workdir and returns its exit code.
// CLIRunner is the v1 implementation; an MCPRunner mapping the same argv onto
// MCP tool calls is planned for M3 — scenario files stay unchanged.
type Runner interface {
	Run(workdir string, argv []string, stdin string, stdout, stderr io.Writer) int
}

// CLIRunner drives the CLI frontend in-process: the full CLI→engine→real-git
// stack. stdin is a non-TTY reader over the caller-supplied string ("" = the
// empty reader), so any engine decision a scenario does not pre-answer via
// flags (or feed via stdin, e.g. `gg batch`) fails deterministically.
type CLIRunner struct{}

func (CLIRunner) Run(workdir string, argv []string, stdin string, stdout, stderr io.Writer) int {
	return cli.Run(workdir, argv, strings.NewReader(stdin), stdout, stderr, "")
}

// ExpandArgs substitutes {{cwd}} in every argument with dir in git slash
// form: filepath.ToSlash (which only rewrites os.PathSeparator, so a
// backslash is also normalized explicitly — this keeps a Windows-style
// drive-letter path correct even under a POSIX test run), with a leading
// "/" prepended when the result does not already start with one (a Windows
// drive letter otherwise reads as a relative path inside a gg:// link). The
// sandbox's path is made at run time, so a scenario that must name an
// absolute path (a gg:// link's local form) has no other way to write it.
// One token, no shell-like expansion: everything else in cmd is literal.
func ExpandArgs(argv []string, dir string) []string {
	slash := strings.ReplaceAll(filepath.ToSlash(dir), `\`, "/")
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = strings.ReplaceAll(a, "{{cwd}}", slash)
	}
	return out
}
