package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func stashGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func stashWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// untrackedStashRepo builds a repo whose stash@{0} carries one tracked
// change and one untracked file (-u), returning the dir and the ^3 sha.
func untrackedStashRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	stashGit(t, dir, "init", "-q", "-b", "main")
	stashGit(t, dir, "config", "user.email", "t@t")
	stashGit(t, dir, "config", "user.name", "t")
	stashWrite(t, dir, "tracked.txt", "one\n")
	stashGit(t, dir, "add", "-A")
	stashGit(t, dir, "commit", "-q", "-m", "base")
	stashWrite(t, dir, "tracked.txt", "two\n")
	stashWrite(t, dir, "brand-new.txt", "u\n")
	stashGit(t, dir, "stash", "push", "-u", "-m", "wip")
	return dir, stashGit(t, dir, "rev-parse", "stash@{0}^3")
}

func TestStashFilesIncludeUntrackedParent(t *testing.T) {
	t.Parallel()
	dir, usha := untrackedStashRepo(t)
	m := Model{svc: domain.Open(dir)}
	msg := m.loadStashFilesCmd("stash@{0}")().(stashFilesMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	var tracked, untracked *contentLine
	for i := range msg.lines {
		switch msg.lines[i].path {
		case "tracked.txt":
			tracked = &msg.lines[i]
		case "brand-new.txt":
			untracked = &msg.lines[i]
		}
	}
	if tracked == nil || untracked == nil {
		t.Fatalf("lines = %+v, want tracked.txt and brand-new.txt", msg.lines)
	}
	if tracked.sha != "" {
		t.Errorf("tracked line sha = %q, want empty (view hash)", tracked.sha)
	}
	if untracked.sha != usha || untracked.status != "A" {
		t.Errorf("untracked line = %+v, want sha %s status A", *untracked, usha)
	}
}

func TestStashFilesPlainStashUnchanged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stashGit(t, dir, "init", "-q", "-b", "main")
	stashGit(t, dir, "config", "user.email", "t@t")
	stashGit(t, dir, "config", "user.name", "t")
	stashWrite(t, dir, "tracked.txt", "one\n")
	stashGit(t, dir, "add", "-A")
	stashGit(t, dir, "commit", "-q", "-m", "base")
	stashWrite(t, dir, "tracked.txt", "two\n")
	stashGit(t, dir, "stash", "push", "-m", "wip") // no -u: no ^3 parent
	m := Model{svc: domain.Open(dir)}
	msg := m.loadStashFilesCmd("stash@{0}")().(stashFilesMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if len(msg.lines) != 1 || msg.lines[0].path != "tracked.txt" || msg.lines[0].sha != "" {
		t.Fatalf("plain stash lines = %+v, want the single tracked file with no sha override", msg.lines)
	}
}

func TestStashUntrackedDiffOpensAgainstParent(t *testing.T) {
	t.Parallel()
	dir, usha := untrackedStashRepo(t)
	m := Model{width: 100, height: 40, sel: map[panel]int{}, svc: domain.Open(dir)}
	m.filesHash = stashGit(t, dir, "rev-parse", "stash@{0}")
	m.filesView = &contentPopup{}
	mm, _ := m.openDiffForFileLine(contentLine{path: "brand-new.txt", status: "A", sha: usha})
	got := mm.(Model)
	if want := "commit:" + usha + ":brand-new.txt"; got.diffTag != want {
		t.Errorf("diffTag = %q, want %q", got.diffTag, want)
	}
}
