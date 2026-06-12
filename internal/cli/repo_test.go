package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gigagit/gg/internal/repos"
)

// withState points the package at a temp registry for one test.
// Tests using this must NOT call t.Parallel() — RepoStatePath is a
// package-level var.
func withState(t *testing.T) string {
	t.Helper()
	state := filepath.Join(t.TempDir(), "repos.toml")
	old := RepoStatePath
	RepoStatePath = state
	t.Cleanup(func() { RepoStatePath = old })
	return state
}

func TestRepoListMRUFirst(t *testing.T) {
	state := withState(t)
	a, b := t.TempDir(), t.TempDir()
	_ = repos.Touch(state, a, time.Unix(1000, 0))
	_ = repos.Touch(state, b, time.Unix(2000, 0))

	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"repo", "list"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 || !strings.Contains(lines[0], b) || !strings.Contains(lines[1], a) {
		t.Fatalf("list not MRU-first:\n%s", out.String())
	}
	if !strings.Contains(lines[0], filepath.Base(b)+"\t") {
		t.Fatalf("expected <name>\\t<path> format: %q", lines[0])
	}
}

func TestRepoSwitchUniqueMatchWritesCwdFile(t *testing.T) {
	state := withState(t)
	target := filepath.Join(t.TempDir(), "unique-zebra")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = repos.Touch(state, target, time.Unix(1000, 0))

	dir := newCLIRepo(t)
	cwdFile := filepath.Join(t.TempDir(), "cwd")
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"repo", "switch", "zebra"}, strings.NewReader(""), &out, &errb, cwdFile); code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), target) {
		t.Fatalf("stdout should print the path, got %q", out.String())
	}
	got, err := os.ReadFile(cwdFile)
	if err != nil || strings.TrimSpace(string(got)) != target {
		t.Fatalf("cwd-file = %q (%v), want %q", got, err, target)
	}
}

func TestRepoSwitchNoMatchErrors(t *testing.T) {
	withState(t)
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"repo", "switch", "nope"}, strings.NewReader(""), &out, &errb, ""); code == 0 {
		t.Fatal("no match should exit non-zero")
	}
	if !strings.Contains(errb.String(), "no known repository") {
		t.Fatalf("stderr should explain, got %q", errb.String())
	}
}

func TestRepoSwitchAmbiguousListsCandidates(t *testing.T) {
	state := withState(t)
	a := filepath.Join(t.TempDir(), "svc-alpha")
	b := filepath.Join(t.TempDir(), "svc-beta")
	for _, p := range []string{a, b} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = repos.Touch(state, p, time.Unix(1000, 0))
	}
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"repo", "switch", "svc"}, strings.NewReader(""), &out, &errb, ""); code == 0 {
		t.Fatal("ambiguous match should exit non-zero")
	}
	if !strings.Contains(errb.String(), "svc-alpha") || !strings.Contains(errb.String(), "svc-beta") {
		t.Fatalf("stderr should list candidates, got %q", errb.String())
	}
}

func TestAnyCommandTouchesRegistry(t *testing.T) {
	state := withState(t)
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"status"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("status exit = %d, stderr=%s", code, errb.String())
	}
	entries := repos.Load(state)
	if len(entries) != 1 {
		t.Fatalf("running a command should record the repo: %+v", entries)
	}
	wantR, _ := filepath.EvalSymlinks(dir)
	gotR, _ := filepath.EvalSymlinks(entries[0].Path)
	if gotR != wantR {
		t.Fatalf("recorded %q, want %q", entries[0].Path, dir)
	}
}

func TestRepoUnknownSubcommand(t *testing.T) {
	withState(t)
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"repo", "bogus"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatal("unknown repo subcommand should exit 2")
	}
}
