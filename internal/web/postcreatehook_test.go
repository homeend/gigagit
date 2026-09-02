package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// The worktree post-create hook is READ from the same file it is WRITTEN to.
// Settings writes go to the ACTIVE repo config (the machine-local private
// file when one exists, else the committed .gg.toml); the web's worktree
// ops used to read the committed file only, so a repo on a private config
// saw its hook in the settings panel and never had it run.
func TestPostCreateHookReadsActivePrivateConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := newRepoDir(t, 1)
	resolved := dir
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		resolved = real
	}
	priv := config.PrivateRepoPath(resolved)
	if err := os.MkdirAll(filepath.Dir(priv), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.SetWorktreePostCreateHook(priv, "echo private-hook"); err != nil {
		t.Fatal(err)
	}
	srv := New(domain.Open(dir))
	req := httptest.NewRequest("GET", "/", nil)
	// The TOML multi-line literal keeps its trailing newline through Load
	// (pinned by config's TestLoadPostCreateHookMultiline); the shell does
	// not care, and neither does this test.
	if got := strings.TrimSpace(srv.postCreateHook(req)); got != "echo private-hook" {
		t.Fatalf("postCreateHook = %q, want the hook from the active private config", got)
	}
	// With no private file the committed .gg.toml is still the source.
	if err := os.Remove(priv); err != nil {
		t.Fatal(err)
	}
	if err := config.SetWorktreePostCreateHook(filepath.Join(dir, ".gg.toml"), "echo committed-hook"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(srv.postCreateHook(req)); got != "echo committed-hook" {
		t.Fatalf("postCreateHook = %q, want the hook from the committed .gg.toml", got)
	}
}
