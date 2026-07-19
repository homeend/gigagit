package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// seedShelfFile shelves a.txt's unstaged working copy into the default bucket.
func seedShelfFile(t *testing.T, e *testEnv) model.ShelfEntry {
	t.Helper()
	entry, err := e.svc.ShelfAdd(context.Background(), model.FileAddress{
		State: model.StateUnstaged, Worktree: e.dir, Branch: "main", Path: "a.txt",
	}, "default")
	if err != nil {
		t.Fatalf("ShelfAdd: %v", err)
	}
	return entry
}

// seedShelfCommit shelves the seed commit as a commit entry.
func seedShelfCommit(t *testing.T, e *testEnv) model.ShelfEntry {
	t.Helper()
	entry, err := e.svc.ShelfAddCommit(context.Background(), e.sha, "seed label")
	if err != nil {
		t.Fatalf("ShelfAddCommit: %v", err)
	}
	return entry
}

func TestShelfBucketsAndList(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e)
	ce := seedShelfCommit(t, e)

	buckets := e.call(t, "gg_shelf_buckets", nil)
	bs := buckets["buckets"].([]any)
	if len(bs) == 0 {
		t.Fatalf("buckets = %v", buckets)
	}

	out := e.call(t, "gg_shelf_list", nil) // default bucket
	rows := out["entries"].([]any)
	if len(rows) != 2 {
		t.Fatalf("entries = %v", rows)
	}
	kinds := map[string]string{}
	for _, r := range rows {
		row := r.(map[string]any)
		kinds[row["id"].(string)] = row["kind"].(string)
	}
	if kinds[fe.ID] != "file" || kinds[ce.ID] != "commit" {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestShelfListNegativeSkipDoesNotPanic(t *testing.T) {
	e := newTestEnv(t)
	seedShelfFile(t, e)
	out := e.call(t, "gg_shelf_list", map[string]any{"skip": -5})
	rows := out["entries"].([]any)
	if len(rows) != 1 {
		t.Fatalf("entries = %v", rows)
	}
}

func TestShelfCommitFiles(t *testing.T) {
	e := newTestEnv(t)
	ce := seedShelfCommit(t, e)
	out := e.call(t, "gg_shelf_commit_files", map[string]any{"id": ce.ID})
	files := out["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	f := files[0].(map[string]any)
	if f["path"] != "a.txt" {
		t.Fatalf("file = %v", f)
	}
}

func TestShelfCommitFilesOnFileEntry(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e)
	msg := e.callErr(t, "gg_shelf_commit_files", map[string]any{"id": fe.ID})
	if !strings.Contains(msg, "file entry") {
		t.Fatalf("error = %s", msg)
	}
}

func TestShelfReadFileEntry(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e)
	out := e.call(t, "gg_shelf_read", map[string]any{"id": fe.ID})
	if out["text"] != "hello\nworld\n" {
		t.Fatalf("text = %q", out["text"])
	}
}

func TestShelfReadCommitMember(t *testing.T) {
	e := newTestEnv(t)
	ce := seedShelfCommit(t, e)
	out := e.call(t, "gg_shelf_read", map[string]any{"id": ce.ID, "member": "a.txt"})
	if out["text"] != "hello\nworld\n" {
		t.Fatalf("member text = %q", out["text"])
	}
}

func TestShelfReadCommitWithoutMemberRefused(t *testing.T) {
	e := newTestEnv(t)
	ce := seedShelfCommit(t, e)
	msg := e.callErr(t, "gg_shelf_read", map[string]any{"id": ce.ID})
	if !strings.Contains(msg, "gg_shelf_commit_files") {
		t.Fatalf("refusal must hint the member list: %s", msg)
	}
}

func TestShelfReadFileEntryWithMemberRefused(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e)
	msg := e.callErr(t, "gg_shelf_read", map[string]any{"id": fe.ID, "member": "a.txt"})
	if !strings.Contains(msg, "omit member") {
		t.Fatalf("error = %s", msg)
	}
}

func TestShelfReadUnknownID(t *testing.T) {
	e := newTestEnv(t)
	msg := e.callErr(t, "gg_shelf_read", map[string]any{"id": "nope"})
	if !strings.Contains(msg, "not found") {
		t.Fatalf("error = %s", msg)
	}
}
