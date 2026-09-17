package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareTreesWorktreeVsCommit(t *testing.T) {
	e := newTestEnv(t)
	// change a.txt in the working tree
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, "gg_compare_trees", map[string]any{
		"left":  map[string]any{"kind": "commit", "rev": "HEAD"},
		"right": map[string]any{"kind": "worktree"},
	})
	files := out["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	f := files[0].(map[string]any)
	if f["path"] != "a.txt" || f["status"] != "M" {
		t.Fatalf("file = %v", f)
	}
	if out["left_display"] == "" || out["right_display"] != "worktree" {
		t.Fatalf("displays = %v / %v", out["left_display"], out["right_display"])
	}
}

// A commit side must survive core.abbrev below Endpoint's 7-char floor.
// svc.CommitLookup hands back model.LogLine.Hash — `%h`, whose width honours
// core.abbrev (git's legal minimum is 4) — so building the Endpoint from it
// made a legal repo config a hard failure: `resolving "HEAD": bad endpoint:
// "dead" is not a commit id (want 7 to 64 hex characters)`. The rev is
// resolved to its FULL sha instead.
func TestCompareTreesShortAbbrev(t *testing.T) {
	e := newTestEnv(t)
	gitRun(t, e.dir, "config", "core.abbrev", "4")
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, "gg_compare_trees", map[string]any{
		"left":  map[string]any{"kind": "commit", "rev": "HEAD"},
		"right": map[string]any{"kind": "worktree"},
	})
	files := out["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	if !strings.Contains(out["left_display"].(string), "seed: a.txt") {
		t.Fatalf("left_display = %q, want the commit subject", out["left_display"])
	}
}

func TestCompareTreesBadRev(t *testing.T) {
	e := newTestEnv(t)
	msg := e.callErr(t, "gg_compare_trees", map[string]any{
		"left":  map[string]any{"kind": "commit", "rev": "no-such-rev"},
		"right": map[string]any{"kind": "worktree"},
	})
	if !strings.Contains(msg, "unknown revision") {
		t.Fatalf("error = %s", msg)
	}
}

func TestCompareTreesBadKind(t *testing.T) {
	e := newTestEnv(t)
	msg := e.callErr(t, "gg_compare_trees", map[string]any{
		"left":  map[string]any{"kind": "banana"},
		"right": map[string]any{"kind": "worktree"},
	})
	if !strings.Contains(msg, `"worktree", "index", or "commit"`) {
		t.Fatalf("error = %s", msg)
	}
}

func TestCompareFileCommitVsWorktree(t *testing.T) {
	e := newTestEnv(t)
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, "gg_compare_file", map[string]any{
		"left":  map[string]any{"source": "commit", "locator": "HEAD", "path": "a.txt"},
		"right": map[string]any{"source": "unstaged", "path": "a.txt"},
	})
	if out["identical"] == true {
		t.Fatal("files differ")
	}
	diff := out["unified_diff"].(string)
	if !strings.Contains(diff, "-world") || !strings.Contains(diff, "+changed") {
		t.Fatalf("diff = %s", diff)
	}
	if strings.Contains(diff, "ui-state") || strings.Contains(diff, os.TempDir()) {
		t.Fatalf("diff leaks temp paths: %s", diff)
	}
	if !strings.Contains(diff, "--- "+out["left_display"].(string)) {
		t.Fatalf("diff header must carry the display label: %s", diff)
	}
}

func TestCompareFileIdentical(t *testing.T) {
	e := newTestEnv(t)
	out := e.call(t, "gg_compare_file", map[string]any{
		"left":  map[string]any{"source": "commit", "locator": "HEAD", "path": "a.txt"},
		"right": map[string]any{"source": "unstaged", "path": "a.txt"},
	})
	if out["identical"] != true {
		t.Fatalf("expected identical, got %v", out)
	}
}

func TestCompareFileBookmarkSide(t *testing.T) {
	e := newTestEnv(t)
	b := seedBookmark(t, e)
	out := e.call(t, "gg_compare_file", map[string]any{
		"left":  map[string]any{"source": "bookmark", "id": b.ID},
		"right": map[string]any{"source": "unstaged", "path": "a.txt"},
	})
	if out["identical"] != true {
		t.Fatalf("bookmark == worktree here, got %v", out)
	}
}

func TestCompareFileBinary(t *testing.T) {
	e := newTestEnv(t)
	if err := os.WriteFile(filepath.Join(e.dir, "bin.dat"), []byte{0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, "bin2.dat"), []byte{0x00, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, "gg_compare_file", map[string]any{
		"left":  map[string]any{"source": "unstaged", "path": "bin.dat"},
		"right": map[string]any{"source": "unstaged", "path": "bin2.dat"},
	})
	if out["binary"] != true {
		t.Fatalf("expected binary flag: %v", out)
	}
}

func TestCompareFileBodyLinesResemblingHeadersSurvive(t *testing.T) {
	e := newTestEnv(t)
	if err := os.WriteFile(filepath.Join(e.dir, "q.sql"), []byte("-- old comment\nselect 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, "q2.sql"), []byte("-- new comment\nselect 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, "gg_compare_file", map[string]any{
		"left":  map[string]any{"source": "unstaged", "path": "q.sql"},
		"right": map[string]any{"source": "unstaged", "path": "q2.sql"},
	})
	diff := out["unified_diff"].(string)
	if !strings.Contains(diff, "--- old comment") {
		t.Fatalf("removed body line resembling a header was corrupted: %s", diff)
	}
	if !strings.Contains(diff, "+-- new comment") {
		t.Fatalf("added body line lost: %s", diff)
	}
	if got := strings.Count(diff, "--- "+out["left_display"].(string)); got != 1 {
		t.Fatalf("expected exactly 1 relabelled header, got %d: %s", got, diff)
	}
}

// gg_compare_trees puts whatever kind the client names into whatever slot it
// named, with no ordering constraint — so the reversed pair is reachable by an
// agent on its first try. It used to answer
// "comparing: DiffTreeFiles: unsupported endpoint pair 1 → 3" while
// `gg compare @worktree main` compared: one compare frontend disagreeing with
// another about what a user may ask. The tool routes through the algebra now,
// so the pair answers, with the statuses read from the LEFT side towards the
// right.
func TestCompareTreesReversedPairCompares(t *testing.T) {
	e := newTestEnv(t)
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, "fresh.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, "gg_compare_trees", map[string]any{
		"left":  map[string]any{"kind": "worktree"},
		"right": map[string]any{"kind": "commit", "rev": "HEAD"},
	})
	got := map[string]string{}
	for _, f := range out["files"].([]any) {
		row := f.(map[string]any)
		got[row["path"].(string)] = row["status"].(string)
	}
	if got["a.txt"] != "M" {
		t.Errorf("a.txt = %q, want M; full result %v", got["a.txt"], got)
	}
	// Forward this is an addition; reversed the comparison ENDS at the commit,
	// where the untracked file does not exist.
	if got["fresh.txt"] != "D" {
		t.Errorf("fresh.txt = %q, want D (untracked on disk, absent at HEAD); full result %v", got["fresh.txt"], got)
	}
}

// index → commit is the other reversed live form, and the one an agent reaches
// for when it wants "what is staged, relative to HEAD" the wrong way round.
func TestCompareTreesReversedIndexAgainstCommit(t *testing.T) {
	e := newTestEnv(t)
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nstaged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, e.dir, "add", "a.txt")
	out := e.call(t, "gg_compare_trees", map[string]any{
		"left":  map[string]any{"kind": "index"},
		"right": map[string]any{"kind": "commit", "rev": "HEAD"},
	})
	files := out["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %v, want exactly a.txt", files)
	}
	f := files[0].(map[string]any)
	if f["path"] != "a.txt" || f["status"] != "M" {
		t.Fatalf("file = %v, want a.txt M", f)
	}
}
