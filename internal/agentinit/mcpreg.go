package agentinit

import (
	"fmt"
	"runtime"
	"strings"
)

// RegisterClaudeMCP registers ggBin's MCP server with Claude Code at user
// scope through Claude's own CLI (`claude mcp add`), so an agent running in a
// gg console gets gg's agent tools. An existing "gg" entry is left alone.
func RegisterClaudeMCP(lookPath func(string) (string, error), run func(name string, args ...string) ([]byte, error), ggBin string) (string, error) {
	manual := "claude mcp add -s user gg -- " + shellArg(ggBin, runtime.GOOS == "windows") + " mcp"
	claude, err := lookPath("claude")
	if err != nil {
		return "", fmt.Errorf("claude is not on PATH — register by hand: %s", manual)
	}
	if _, err := run(claude, "mcp", "get", "gg"); err == nil {
		return "already registered", nil
	}
	if out, err := run(claude, "mcp", "add", "-s", "user", "gg", "--", ggBin, "mcp"); err != nil {
		return "", fmt.Errorf("claude mcp add failed (%v): %s — register by hand: %s", err, strings.TrimSpace(string(out)), manual)
	}
	return "registered", nil
}

// shellArg quotes s for the user's shell when it needs it: double quotes for
// cmd/PowerShell, POSIX single quotes elsewhere.
func shellArg(s string, windows bool) string {
	plain := s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(`/\._-:+=@%,`, r))
	}) < 0
	switch {
	case plain:
		return s
	case windows:
		return `"` + s + `"`
	default:
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
}
