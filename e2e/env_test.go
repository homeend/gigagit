package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain pins git's environment process-wide so both the builder's git
// calls and gg's own git invocations (via cli.Run) are isolated from the
// machine: no system/user gitconfig, fixed identity, file-protocol clones
// allowed, and gg's global config redirected away from the real user's.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gg-e2e-env")
	if err != nil {
		panic(err)
	}
	gcfg := filepath.Join(dir, "gitconfig")
	cfg := "[init]\n\tdefaultBranch = main\n" +
		"[user]\n\tname = gg-e2e\n\temail = e2e@gg\n" +
		"[protocol \"file\"]\n\tallow = always\n"
	if err := os.WriteFile(gcfg, []byte(cfg), 0o644); err != nil {
		panic(err)
	}
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Setenv("GIT_CONFIG_GLOBAL", gcfg)
	os.Setenv("GIT_AUTHOR_NAME", "gg-e2e")
	os.Setenv("GIT_AUTHOR_EMAIL", "e2e@gg")
	os.Setenv("GIT_COMMITTER_NAME", "gg-e2e")
	os.Setenv("GIT_COMMITTER_EMAIL", "e2e@gg")
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg")) // gg global config isolation
	os.Unsetenv("GIT_DIR")                                  // ambient GIT_DIR would redirect every git call
	code := func() int {
		defer os.RemoveAll(dir)
		return m.Run()
	}()
	os.Exit(code)
}
