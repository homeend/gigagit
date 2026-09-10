package mcp

import (
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

func TestNotifyNotesChangedPostsForALiveSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := &Server{steerDir: dir} // the injected seam; no env, so this stays parallel
	s.notifyNotesChanged()
	if got := steer.Drain(dir); len(got) != 0 {
		t.Fatalf("posted %+v with no live presence, want nothing", got)
	}
	if err := steer.Touch(dir, steer.TUIPresence, steer.Presence{PID: 1, Worktree: "/w"}); err != nil {
		t.Fatal(err)
	}
	s.notifyNotesChanged()
	got := steer.Drain(dir)
	if len(got) != 1 || got[0].Cmd != "reload" || got[0].Sources[0] != "notes" {
		t.Fatalf("posted %+v, want one reload notes", got)
	}
}

func TestNotifyNotesChangedWithNoInboxIsANoOp(t *testing.T) {
	t.Parallel()
	(&Server{}).notifyNotesChanged() // must not panic
}
