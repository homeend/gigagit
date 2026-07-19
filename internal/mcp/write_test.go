package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRestoreShelfFileDefaultPath(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e) // shelved a.txt = "hello\nworld\n"
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("clobbered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.call(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": fe.ID}, "overwrite": true,
	})
	if out["path"] != "a.txt" {
		t.Fatalf("default path must be the origin path: %v", out["path"])
	}
	data, _ := os.ReadFile(filepath.Join(e.dir, "a.txt"))
	if string(data) != "hello\nworld\n" {
		t.Fatalf("restored content = %q", data)
	}
}

func TestWriteExplicitPathAndMember(t *testing.T) {
	e := newTestEnv(t)
	ce := seedShelfCommit(t, e)
	out := e.call(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": ce.ID, "member": "a.txt"},
		"path":   "restored/copy.txt",
	})
	if out["path"] != "restored/copy.txt" {
		t.Fatalf("path = %v", out["path"])
	}
	data, err := os.ReadFile(filepath.Join(e.dir, "restored", "copy.txt"))
	if err != nil || string(data) != "hello\nworld\n" {
		t.Fatalf("member write: %q err=%v", data, err)
	}
}

func TestWriteBookmarkPaste(t *testing.T) {
	e := newTestEnv(t)
	b := seedBookmark(t, e) // committed a.txt bookmark
	out := e.call(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"bookmark": b.ID}, "path": "pasted.txt",
	})
	if out["bytes"].(float64) != 12 {
		t.Fatalf("bytes = %v", out["bytes"])
	}
	if _, err := os.Stat(filepath.Join(e.dir, "pasted.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteOverwriteRefusedThenAccepted(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e)
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("different\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := e.callErr(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": fe.ID},
	})
	if !strings.Contains(msg, "overwrite:true") {
		t.Fatalf("refusal must name the fix: %s", msg)
	}
	out := e.call(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": fe.ID}, "overwrite": true,
	})
	if out["unchanged"] == true {
		t.Fatalf("overwrite write must report a real write: %v", out)
	}
}

func TestWriteUnchangedNoOp(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e) // a.txt already has identical content
	out := e.call(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": fe.ID},
	})
	if out["unchanged"] != true {
		t.Fatalf("identical bytes must be a reported no-op: %v", out)
	}
}

func TestWriteRefusals(t *testing.T) {
	e := newTestEnv(t)
	ce := seedShelfCommit(t, e)
	fe := seedShelfFile(t, e)

	msg := e.callErr(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": ce.ID},
	})
	if !strings.Contains(msg, "gg_shelf_commit_files") {
		t.Fatalf("commit entry without member: %s", msg)
	}
	msg = e.callErr(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": fe.ID, "member": "a.txt"},
	})
	if !strings.Contains(msg, "omit member") {
		t.Fatalf("file entry with member: %s", msg)
	}
	sha := gitRun(t, e.dir, "rev-parse", "HEAD")
	cb := seedCommitBookmark(t, e, sha)
	msg = e.callErr(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"bookmark": cb.ID},
	})
	if !strings.Contains(msg, "gg_cherry_pick") {
		t.Fatalf("commit bookmark refusal must hint gg_cherry_pick: %s", msg)
	}
	msg = e.callErr(t, "gg_write_to_worktree", map[string]any{"source": map[string]any{}})
	if !strings.Contains(msg, "exactly one") {
		t.Fatalf("source validation: %s", msg)
	}
	msg = e.callErr(t, "gg_write_to_worktree", map[string]any{
		"source": map[string]any{"shelf": fe.ID}, "path": "../outside.txt",
	})
	if msg == "" {
		t.Fatalf("outside-tree path must be rejected")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "..", "outside.txt")); err == nil {
		t.Fatalf("outside-tree file was written")
	}
}
