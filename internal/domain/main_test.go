package domain

import (
	"os"
	"testing"
)

// TestMain points XDG_CONFIG_HOME at an empty directory for the whole package:
// preflight probes the global config for stored old review commands
// (structured-reviews), so the developer's own config must never steer the
// suite. A test that needs its own config dir overrides it with t.Setenv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gg-domain-xdg")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	// The session registry lives under XDG state: no test may read (or
	// sweep) the developer's real one.
	state, err := os.MkdirTemp("", "gg-domain-state")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", state)
	code := m.Run()
	os.RemoveAll(state)
	os.RemoveAll(dir)
	os.Exit(code)
}
