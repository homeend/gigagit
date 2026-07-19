package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// seedBookmark stores a committed-file bookmark for a.txt at the seed commit.
func seedBookmark(t *testing.T, e *testEnv) model.Bookmark {
	t.Helper()
	b, err := e.svc.BookmarkAdd(context.Background(), model.Bookmark{
		State: model.StateCommitted, Commit: e.sha, Path: "a.txt",
	})
	if err != nil {
		t.Fatalf("BookmarkAdd: %v", err)
	}
	return b
}

func TestBookmarksListAndGet(t *testing.T) {
	e := newTestEnv(t)
	b := seedBookmark(t, e)

	out := e.call(t, "gg_bookmarks_list", nil)
	rows, ok := out["bookmarks"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("bookmarks = %v", out["bookmarks"])
	}
	row := rows[0].(map[string]any)
	if row["id"] != b.ID || row["state"] != "committed" || row["path"] != "a.txt" {
		t.Fatalf("row = %v", row)
	}
	if row["display"] == "" {
		t.Fatal("display missing")
	}

	got := e.call(t, "gg_bookmark_get", map[string]any{"id": b.ID})
	bk := got["bookmark"].(map[string]any)
	if bk["commit"] != e.sha {
		t.Fatalf("get.commit = %v", bk["commit"])
	}
}

func TestBookmarksListNegativeSkipDoesNotPanic(t *testing.T) {
	e := newTestEnv(t)
	seedBookmark(t, e)
	out := e.call(t, "gg_bookmarks_list", map[string]any{"skip": -5})
	rows, ok := out["bookmarks"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("bookmarks = %v", out["bookmarks"])
	}
}

func TestBookmarkGetUnknownID(t *testing.T) {
	e := newTestEnv(t)
	msg := e.callErr(t, "gg_bookmark_get", map[string]any{"id": "nope"})
	if !strings.Contains(msg, "bookmark not found") {
		t.Fatalf("error = %s", msg)
	}
}

func TestBookmarkRead(t *testing.T) {
	e := newTestEnv(t)
	b := seedBookmark(t, e)
	out := e.call(t, "gg_bookmark_read", map[string]any{"id": b.ID})
	if out["text"] != "hello\nworld\n" {
		t.Fatalf("text = %q", out["text"])
	}
	if out["size"].(float64) != 12 {
		t.Fatalf("size = %v", out["size"])
	}
}

func TestBookmarkReadCommitPointerRefused(t *testing.T) {
	e := newTestEnv(t)
	cb, err := e.svc.BookmarkAdd(context.Background(), model.Bookmark{
		State: model.StateCommitted, Commit: e.sha, // no Path → commit pointer
	})
	if err != nil {
		t.Fatal(err)
	}
	msg := e.callErr(t, "gg_bookmark_read", map[string]any{"id": cb.ID})
	if !strings.Contains(msg, "gg_export") {
		t.Fatalf("commit-pointer refusal must hint gg_export: %s", msg)
	}
}
