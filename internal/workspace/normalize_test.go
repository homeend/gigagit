package workspace

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// linkedTree writes the on-disk shape of a linked worktree of the repository
// at main: <main>/.git/worktrees/<name> and a `.git` file pointing into it.
func linkedTree(t *testing.T, main, name string) string {
	t.Helper()
	gitdir := filepath.Join(main, ".git", "worktrees", name)
	if err := os.MkdirAll(gitdir, 0o755); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return wt
}

func TestNormalizeEntryLinkedWorktreeToMain(t *testing.T) {
	t.Parallel()
	main := t.TempDir()
	wt := linkedTree(t, main, "feat")
	got := NormalizeEntry(Entry{Name: "w", SettingsRepo: wt, Repos: []string{wt}})
	if !reflect.DeepEqual(got.Repos, []string{main}) {
		t.Fatalf("repos = %v, want the main checkout %s", got.Repos, main)
	}
	if got.SettingsRepo != main {
		t.Fatalf("settings repo = %q, want it to follow to %s", got.SettingsRepo, main)
	}
}

func TestNormalizeEntryDedupesSymlink(t *testing.T) {
	t.Parallel()
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
	got := NormalizeEntry(Entry{Name: "w", SettingsRepo: link, Repos: []string{real, link}})
	if !reflect.DeepEqual(got.Repos, []string{real}) {
		t.Fatalf("repos = %v, want the first spelling only", got.Repos)
	}
	if got.SettingsRepo != "" {
		t.Fatalf("settings repo = %q, want it cleared with its member", got.SettingsRepo)
	}
}

func TestNormalizeEntriesKeepsOrder(t *testing.T) {
	t.Parallel()
	got := NormalizeEntries([]Entry{{Name: "a", Repos: []string{"/x"}}, {Name: "b", Repos: []string{"/y"}}})
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("got %+v", got)
	}
}

func TestHeadingLabelsDisambiguateEqualNames(t *testing.T) {
	t.Parallel()
	got := HeadingLabels([]string{"api", "api", "web"}, []string{"/w/shop/api", "/w/billing/api", "/w/web"})
	want := []string{"api (shop)", "api (billing)", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
