package tui

import (
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
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

func bgNav(id, file string, line int) steer.Command {
	c := steer.Command{ID: id, Cmd: "navigate", File: file, Background: true,
		Target: &steer.Target{State: "unstaged"}, HintKind: model.ContentHintKind, HintID: model.ContentHintID, Wait: true}
	if line > 0 {
		c.Line = &steer.Line{Side: "new", No: line}
	}
	return c
}

func TestBackgroundNavigateLoadsWithoutAFrame(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.filterTyping = true // not refused: nothing on screen moves
	nm, cmd := m.applySteer(bgNav("b-1", "a.txt", 30))
	nm = pumpAll(t, nm, cmd)
	if layerOf[*fileViewer](nm) != nil || nm.filesPreview != nil {
		t.Fatal("a background open put the file on screen")
	}
	d := nm.openFiles.find(nm.currentWorktree, docKey(fileSource{kind: srcWorktree}, "a.txt"))
	if d == nil || !docLoaded(d) || d.p.cur != 29 {
		t.Fatalf("doc = %+v, want a.txt loaded with the cursor on line 30", d)
	}
	r, ok := steer.AwaitReply(nm.steerDir, "b-1", time.Second)
	if !ok || !r.OK || r.Detail != "opened a.txt in the background at line 30" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestBackgroundNavigateClampsAndNamesTheEviction(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	fill20(m)
	nm, cmd := m.applySteer(bgNav("b-2", "a.txt", 99))
	nm = pumpAll(t, nm, cmd)
	r, ok := steer.AwaitReply(nm.steerDir, "b-2", time.Second)
	want := "opened a.txt in the background at line 40 (line 99 is past the end, 40 lines); closed f0.txt (20 files open)"
	if !ok || !r.OK || r.Detail != want {
		t.Fatalf("reply = %+v ok=%v, want %q", r, ok, want)
	}
}

func TestBackgroundNavigateIsNotBlockedByAParkedNavigate(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.pendingSteer = &pendingSteer{cmd: steer.Command{ID: "other"}, stage: steerStageDiff, at: time.Now()}
	nm, cmd := m.applySteer(bgNav("b-3", "a.txt", 0))
	nm = pumpAll(t, nm, cmd)
	r, ok := steer.AwaitReply(nm.steerDir, "b-3", time.Second)
	if !ok || !r.OK || r.Detail != "opened a.txt in the background" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestBackgroundNavigateOfAShownFileMovesNothing(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 40)
	d.p.cur = 2
	m = m.pushLayer(&fileViewer{d})
	nm, cmd := m.applySteer(bgNav("b-4", "a.txt", 30))
	nm = pumpAll(t, nm, cmd)
	if d.p.cur != 2 {
		t.Errorf("cursor moved to %d on a file the user is reading", d.p.cur+1)
	}
	r, ok := steer.AwaitReply(nm.steerDir, "b-4", time.Second)
	if !ok || !r.OK || r.Detail != "a.txt is already open on screen" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestBackgroundNavigateRefusals(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	if m.snapshotWorktree == "" {
		m.snapshotWorktree = m.currentWorktree
	}
	other := bgNav("r-2", "a.txt", 0)
	other.Worktree = "/elsewhere"
	for _, tc := range []struct {
		c    steer.Command
		want string
	}{
		{bgNav("r-1", "gone.txt", 0), "gone.txt is not in the working tree"},
		{other, "gg is showing worktree " + m.snapshotWorktree + ", not /elsewhere"},
	} {
		nm, cmd := m.applySteer(tc.c)
		runSteerCmd(t, cmd)
		r, ok := steer.AwaitReply(nm.steerDir, tc.c.ID, time.Second)
		if !ok || r.OK || r.Error != tc.want {
			t.Errorf("%s: reply = %+v ok=%v, want %q", tc.c.ID, r, ok, tc.want)
		}
		if nm.steerAsk != nil {
			t.Errorf("%s: a background open asked the user to switch", tc.c.ID)
		}
	}
}

func TestFileFocusBringsACommitVersionToTheFrontAtALine(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	d := bgDoc(m, fileSource{kind: srcCommit, rev: "abc"}, "b.txt", 10)
	nm, cmd := m.applySteer(steer.Command{ID: "ff-1", Cmd: "file_focus", FileID: d.id(), Line: &steer.Line{No: 7}, Wait: true})
	nm = pumpAll(t, nm, cmd)
	fv, ok := nm.topLayer().(*fileViewer)
	if !ok || fv.openFile != d || d.p.cur != 6 {
		t.Fatalf("top = %T cur=%d, want b.txt's viewer on line 7", nm.topLayer(), d.p.cur+1)
	}
	r, ok := steer.AwaitReply(nm.steerDir, "ff-1", time.Second)
	if !ok || !r.OK || r.Detail != "focused b.txt at line 7" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestFileFocusWorktreeFileReloadsAndLandsTheLine(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	nm, cmd := m.applySteer(bgNav("b-5", "a.txt", 0))
	nm = pumpAll(t, nm, cmd)
	steer.AwaitReply(nm.steerDir, "b-5", time.Second)
	nm, cmd = nm.applySteer(steer.Command{ID: "ff-2", Cmd: "file_focus", File: "a.txt", Line: &steer.Line{No: 12}, Wait: true})
	nm = pumpAll(t, nm, cmd)
	fv, ok := nm.topLayer().(*fileViewer)
	if !ok || fv.p.cur != 11 {
		t.Fatalf("top = %T, want a.txt's viewer on line 12", nm.topLayer())
	}
	r, ok := steer.AwaitReply(nm.steerDir, "ff-2", time.Second)
	if !ok || !r.OK || r.Detail != "focused a.txt at line 12" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestFileFocusUnknownAndRefused(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	nm, cmd := m.applySteer(steer.Command{ID: "ff-3", Cmd: "file_focus", FileID: "f424242", Wait: true})
	runSteerCmd(t, cmd)
	if r, ok := steer.AwaitReply(nm.steerDir, "ff-3", time.Second); !ok || r.OK || r.Error != "no open file f424242" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
	bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 3)
	m.filterTyping = true
	nm, cmd = m.applySteer(steer.Command{ID: "ff-4", Cmd: "file_focus", File: "a.txt", Wait: true})
	runSteerCmd(t, cmd)
	if r, ok := steer.AwaitReply(nm.steerDir, "ff-4", time.Second); !ok || r.OK || r.Error != "the user is typing" {
		t.Fatalf("reply = %+v ok=%v, want the typing refusal", r, ok)
	}
}
