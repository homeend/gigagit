package config

import (
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.Worktree.PathTemplate != "../<repo>.worktrees/<branch>" {
		t.Errorf("path default = %q", d.Worktree.PathTemplate)
	}
	if d.Worktree.DefaultBranchTemplate != "b/from-<parent-branch>-<random-alpha:4>" {
		t.Errorf("branch default = %q", d.Worktree.DefaultBranchTemplate)
	}
}

func TestDefaultGlobalPathXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got := DefaultGlobalPath(); got != filepath.Join("/xdg", "gg", "config.toml") {
		t.Errorf("xdg path = %q", got)
	}
}

func TestDefaultGlobalPathHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/u")
	if got := DefaultGlobalPath(); got != filepath.Join("/home/u", ".config", "gg", "config.toml") {
		t.Errorf("home path = %q", got)
	}
}
