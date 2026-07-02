package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestConfigKeysParsesHelpOutput(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git help -c", gitexec.Result{Stdout: "add.ignoreErrors\nuser.name\n\nuser.email\n"})
	r := &Repo{Runner: f}
	keys, err := r.ConfigKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 || keys[0] != "add.ignoreErrors" || keys[2] != "user.email" {
		t.Fatalf("keys = %v, want the 3 non-blank lines in order", keys)
	}
	if got := strings.Join(f.Calls[0].Argv, " "); got != "help -c" {
		t.Fatalf("argv = %q, want 'help -c'", got)
	}
}

func TestConfigKeysRealGit(t *testing.T) {
	_, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	keys, err := r.ConfigKeys(context.Background())
	if err != nil {
		t.Fatalf("ConfigKeys: %v", err)
	}
	if len(keys) < 100 {
		t.Fatalf("expected a big catalog, got %d keys", len(keys))
	}
	found := false
	for _, k := range keys {
		if k == "user.name" {
			found = true
		}
	}
	if !found {
		t.Fatal("catalog must contain user.name")
	}
}

func TestConfigListScopedParsesZFormat(t *testing.T) {
	f := gitexec.NewFakeRunner()
	// Pinned -z format: scope NUL key\nvalue NUL, repeated. Include a system
	// record (dropped) and a multiline value (survives -z).
	raw := "system\x00core.something\ntrue\x00" +
		"global\x00user.name\nAda L\x00" +
		"local\x00core.filemode\nfalse\x00" +
		"local\x00alias.lg\nlog --graph\nall\x00"
	f.SetResponse("git config list", gitexec.Result{Stdout: raw})
	r := &Repo{Runner: f}
	set, err := r.ConfigListScoped(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 3 {
		t.Fatalf("settings = %+v, want 3 (system dropped)", set)
	}
	if set[0].Scope != ConfigGlobal || set[0].Key != "user.name" || set[0].Value != "Ada L" {
		t.Fatalf("first = %+v", set[0])
	}
	if set[2].Value != "log --graph\nall" {
		t.Fatalf("multiline value mangled: %q", set[2].Value)
	}
	if got := strings.Join(f.Calls[0].Argv, " "); got != "config --list --show-scope -z" {
		t.Fatalf("argv = %q", got)
	}
}

func TestConfigListScopedRealGit(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()
	if err := r.ConfigSet(ctx, ConfigGlobal, "user.name", "Global Person"); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigSet(ctx, ConfigLocal, "core.somekey", "v1"); err != nil {
		t.Fatal(err)
	}
	set, err := r.ConfigListScoped(ctx)
	if err != nil {
		t.Fatalf("ConfigListScoped: %v", err)
	}
	var sawGlobal, sawLocal bool
	for _, s := range set {
		if s.Scope == ConfigGlobal && s.Key == "user.name" && s.Value == "Global Person" {
			sawGlobal = true
		}
		if s.Scope == ConfigLocal && s.Key == "core.somekey" && s.Value == "v1" {
			sawLocal = true
		}
	}
	if !sawGlobal || !sawLocal {
		t.Fatalf("missing scoped records: global=%v local=%v in %+v", sawGlobal, sawLocal, set)
	}
	_ = dir
}

func TestConfigUnsetRemovesKeyAndToleratesMissing(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()
	if err := r.ConfigSet(ctx, ConfigLocal, "core.somekey", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigUnset(ctx, ConfigLocal, "core.somekey"); err != nil {
		t.Fatalf("unset existing: %v", err)
	}
	if _, set, _ := r.ConfigGet(ctx, ConfigLocal, "core.somekey"); set {
		t.Fatal("key still set after unset")
	}
	// Unsetting a missing key exits 5 — must be a no-op success.
	if err := r.ConfigUnset(ctx, ConfigLocal, "core.somekey"); err != nil {
		t.Fatalf("unset missing key must be a no-op, got %v", err)
	}
}
