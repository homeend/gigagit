package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
)

// pageConfigChanged is the page having written config: the host calls the
// installed follow from its HTTP goroutine, and Update receives the message.
func pageConfigChanged(t *testing.T, f *fakeWebHost, m Model, relocated bool) webConfigChangedMsg {
	t.Helper()
	if f.configChanged == nil {
		t.Fatal("the TUI installed no config follow on its page")
	}
	f.configChanged(relocated)
	got := make(chan any, 1)
	go func() { got <- waitWebConfigCmd(m.web)() }()
	select {
	case msg := <-got:
		req, ok := msg.(webConfigChangedMsg)
		if !ok || req.relocated != relocated {
			t.Fatalf("msg = %#v", msg)
		}
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("the page's config change never reached Update")
	}
	return webConfigChangedMsg{}
}

// A page relocation (move to private) rebinds the terminal's repo-config
// write target, or its next Settings write would recreate the moved file.
func TestPageRelocationRebindsTheWriteTarget(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := installFakeHost(t)
	m := servingModel(t, f)
	committed := filepath.Join(m.currentWorktree, ".gg.toml")
	m.repoConfigPath = committed
	private := config.PrivateRepoPath(m.worktrees[0].Path)
	if err := os.MkdirAll(filepath.Dir(private), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(private, []byte("[ui]\ncommit_sort = \"plain\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gen := m.loadGen
	nm, cmd := m.Update(pageConfigChanged(t, f, m, true))
	m = nm.(Model)
	if m.repoConfigPath != private {
		t.Fatalf("repoConfigPath = %q, want the private file %q", m.repoConfigPath, private)
	}
	if m.loadGen == gen || cmd == nil {
		t.Fatal("a relocation reloads the terminal (loadGen, loadCmd)")
	}
}

// Tool blocks the page appended are the terminal's at once: its wizard shows
// them configured and never appends them twice.
func TestPageToolsAddReloadsTheConfig(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	f := installFakeHost(t)
	m := servingModel(t, f)
	if err := config.AppendToolCommands(config.DefaultGlobalPath(), []config.ToolCommand{{Category: "review", Name: "Page", Mode: "capture", Command: "x"}}); err != nil {
		t.Fatal(err)
	}
	nm, _ := m.Update(pageConfigChanged(t, f, m, false))
	m = nm.(Model)
	found := false
	for _, tc := range m.cfg.Tools.Command {
		found = found || tc.Name == "Page"
	}
	if !found {
		t.Fatalf("the page's block is not in the terminal's config: %+v", m.cfg.Tools.Command)
	}
}
