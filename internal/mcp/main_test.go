package mcp

import (
	"os"
	"testing"
)

// TestMain points XDG_CONFIG_HOME at an empty directory for the whole package:
// preflight reads the global config (stored old review commands), and no
// test may read or migrate the developer's own config. A test that needs its own config dir overrides it
// with t.Setenv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gg-mcp-xdg")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	// A suite run inside a gg console must not reach that console's TUI.
	os.Unsetenv("GG_MCP_URL")
	os.Unsetenv("GG_SESSION_TOKEN")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
