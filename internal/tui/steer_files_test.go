package tui

import (
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/steer"
)

func TestSteerFilesRepliesWithTheListEvenWhileTheUserTypes(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 3)
	m.filterTyping = true // steerRefusal would say "the user is typing"
	nm, cmd := m.applySteer(steer.Command{ID: "c-f", Cmd: "files", Wait: true})
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(nm.steerDir, "c-f", time.Second)
	if !ok || !r.OK || len(r.Files) != 1 || r.Files[0].Path != "a.txt" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestSteerFilesEmptyListIsOK(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	nm, cmd := m.applySteer(steer.Command{ID: "c-e", Cmd: "files", Wait: true})
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(nm.steerDir, "c-e", time.Second)
	if !ok || !r.OK || len(r.Files) != 0 || r.Detail != "no open files" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestSteerOpenFilesMalformedCommandsAreRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		c    steer.Command
		want string
	}{
		{steer.Command{Cmd: "focus", Panel: "files", Background: true}, "background applies to navigate only"},
		{steer.Command{Cmd: "navigate", File: "a.txt", Background: true}, "background needs a content link"},
		{steer.Command{Cmd: "file_focus"}, "file_focus needs an id or a path"},
		{steer.Command{Cmd: "file_focus", FileID: "f1", Line: &steer.Line{No: -1}}, "a line number is 1-based"},
	} {
		if got := steerEnumRefusal(tc.c); got != tc.want {
			t.Errorf("steerEnumRefusal(%+v) = %q, want %q", tc.c, got, tc.want)
		}
	}
}
