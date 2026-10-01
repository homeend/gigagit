package agentinit

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestRegisterClaudeMCPNoClaude(t *testing.T) {
	ran := false
	_, err := RegisterClaudeMCP(func(string) (string, error) { return "", errors.New("not found") },
		func(string, ...string) ([]byte, error) { ran = true; return nil, nil }, "/bin/gg")
	if err == nil || !strings.Contains(err.Error(), "claude mcp add -s user gg -- /bin/gg mcp") || ran {
		t.Fatalf("err = %v ran = %v", err, ran)
	}
}

func TestRegisterClaudeMCPAddsOnce(t *testing.T) {
	var calls [][]string
	registered := false
	run := func(name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if args[1] == "get" && !registered {
			return []byte("No MCP server found"), errors.New("exit 1")
		}
		if args[1] == "add" {
			registered = true
		}
		return nil, nil
	}
	look := func(string) (string, error) { return "/usr/bin/claude", nil }
	st, err := RegisterClaudeMCP(look, run, "/opt/gg")
	if err != nil || st != "registered" {
		t.Fatalf("first = %q %v", st, err)
	}
	want := []string{"/usr/bin/claude", "mcp", "add", "-s", "user", "gg", "--", "/opt/gg", "mcp"}
	if !slices.Equal(calls[1], want) {
		t.Fatalf("add argv = %v", calls[1])
	}
	st, err = RegisterClaudeMCP(look, run, "/opt/gg")
	if err != nil || st != "already registered" || len(calls) != 3 {
		t.Fatalf("second = %q %v calls %v", st, err, calls)
	}
}
