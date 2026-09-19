package domain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A bookmark is a POINTER: once the commit or blob it names is rebased away and
// gc'd there is nothing left to open. BookmarkProbe is the one check every
// frontend asks before opening one, so they all say the same thing.
func TestBookmarkProbeReportsGoneEntries(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	head := headHash(t, dir)
	const gone = "1f0726ecb1390a292d0e2893b336f067b83895d9"

	live := model.Bookmark{State: model.StateCommitted, Commit: head}
	if err := svc.BookmarkProbe(ctx, live); err != nil {
		t.Fatalf("live commit bookmark: %v", err)
	}
	blob, err := svc.repo.BlobSHA(ctx, head, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	liveFile := model.Bookmark{State: model.StateCommitted, Commit: head, Path: "README.md", SHA: blob}
	if err := svc.BookmarkProbe(ctx, liveFile); err != nil {
		t.Fatalf("live file bookmark: %v", err)
	}

	for name, b := range map[string]model.Bookmark{
		"commit": {State: model.StateCommitted, Commit: gone},
		"file":   {State: model.StateCommitted, Commit: gone, Path: "g.txt", SHA: "3e757656cf36eca53338e520d134963a44f793f8"},
		"live":   {State: model.StateUnstaged, Worktree: dir, Path: "never-existed.txt"},
	} {
		err := svc.BookmarkProbe(ctx, b)
		var ge *EntryGoneError
		if !errors.As(err, &ge) {
			t.Fatalf("%s: err = %v, want an *EntryGoneError", name, err)
		}
		if !strings.Contains(err.Error(), "no longer available") {
			t.Fatalf("%s: message = %q", name, err.Error())
		}
		if ge.Cause == nil {
			t.Fatalf("%s: the raw cause must be kept for the detail line", name)
		}
	}
}
