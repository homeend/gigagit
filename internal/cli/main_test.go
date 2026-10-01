package cli

import (
	"os"
	"testing"
)

// TestMain points XDG_CONFIG_HOME at an empty directory for the whole package:
// preflight reads the global config (stored old review commands), and a
// `gg migrate --yes` test must never rewrite the developer's own config — one
// did, before this existed. A test that needs its own config dir overrides it
// with t.Setenv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gg-cli-xdg")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	// Every Execute now reads the session registry under XDG state (the
	// recycle guards): no test may read or sweep the developer's real one.
	state, err := os.MkdirTemp("", "gg-cli-state")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", state)
	code := m.Run()
	os.RemoveAll(state)
	os.RemoveAll(dir)
	os.Exit(code)
}
