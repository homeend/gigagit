package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
	"github.com/homeend/gigagit/internal/textdiff"
)

func markCmd(id string, tone string, start, end int) steer.Command {
	return steer.Command{
		ID: id, Cmd: "highlight", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Side:   "new", Start: start, End: end, Tone: tone, Wait: true,
	}
}

func TestSteerHighlightStoresAMarkAndPaintsTheRows(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true

	m, cmd := m.applySteer(markCmd("h-1", "warn", 10, 12))
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(dir, "h-1", time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true", r, ok)
	}

	v := &diffView{noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}}
	for _, tc := range []struct {
		no   int
		want bool
	}{{9, false}, {10, true}, {11, true}, {12, true}, {13, false}} {
		_, got := m.attnMarkFor(v, textdiff.Row{RightNo: tc.no})
		if got != tc.want {
			t.Errorf("new line %d painted = %v, want %v (range is 1-based INCLUSIVE)", tc.no, got, tc.want)
		}
	}
	// A mark on the new side must not paint the old side's numbers.
	if _, got := m.attnMarkFor(v, textdiff.Row{LeftNo: 11}); got {
		t.Error("a new-side mark painted an old-side row")
	}
	// …and not another file's diff.
	other := &diffView{noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "b.txt"}}
	if _, got := m.attnMarkFor(other, textdiff.Row{RightNo: 11}); got {
		t.Error("a mark leaked into another file's diff")
	}
}

func TestSteerHighlightDefaultsAndRefusals(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true

	// end omitted = a single line; side omitted = new.
	one := markCmd("h-2", "info", 5, 0)
	one.Side = ""
	m, cmd := m.applySteer(one)
	runSteerCmd(t, cmd)
	if r, _ := steer.AwaitReply(dir, "h-2", time.Second); !r.OK {
		t.Fatalf("reply = %+v, want ok:true for a one-line mark", r)
	}
	v := &diffView{noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}}
	if _, got := m.attnMarkFor(v, textdiff.Row{RightNo: 5}); !got {
		t.Error("an omitted end must mark exactly the start line on the new side")
	}

	for _, bad := range []struct {
		name string
		cmd  steer.Command
	}{
		{"tone", func() steer.Command { c := markCmd("h-3", "shout", 1, 1); return c }()},
		{"side", func() steer.Command { c := markCmd("h-4", "info", 1, 1); c.Side = "middle"; return c }()},
		{"start", func() steer.Command { c := markCmd("h-5", "info", 0, 3); return c }()},
		{"backwards range", func() steer.Command { c := markCmd("h-6", "info", 9, 4); return c }()},
		{"no file", func() steer.Command { c := markCmd("h-7", "info", 1, 1); c.File = ""; return c }()},
	} {
		bad := bad
		t.Run(bad.name, func(t *testing.T) {
			t.Parallel()
			mm, cmd := m.applySteer(bad.cmd)
			runSteerCmd(t, cmd)
			r, _ := steer.AwaitReply(dir, bad.cmd.ID, time.Second)
			if r.OK {
				t.Errorf("a bad %s was accepted: %+v", bad.name, r)
			}
			_ = mm
		})
	}
}

func TestSteerHighlightClearScopes(t *testing.T) {
	t.Parallel()
	m, _ := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m, c1 := m.applySteer(markCmd("c-1", "info", 1, 2))
	runSteerCmd(t, c1)
	two := markCmd("c-2", "error", 3, 4)
	two.File = "b.txt"
	m, c2 := m.applySteer(two)
	runSteerCmd(t, c2)

	va := &diffView{noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}}
	vb := &diffView{noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "b.txt"}}

	m, c3 := m.applySteer(steer.Command{ID: "c-3", Cmd: "highlight_clear", File: "a.txt", Target: &steer.Target{State: "unstaged"}, Wait: true})
	runSteerCmd(t, c3)
	if _, got := m.attnMarkFor(va, textdiff.Row{RightNo: 1}); got {
		t.Error("clear with a file must drop that file's marks")
	}
	if _, got := m.attnMarkFor(vb, textdiff.Row{RightNo: 3}); !got {
		t.Error("clear with a file must leave other files' marks alone")
	}

	m, c4 := m.applySteer(steer.Command{ID: "c-4", Cmd: "highlight_clear", Wait: true})
	runSteerCmd(t, c4)
	if _, got := m.attnMarkFor(vb, textdiff.Row{RightNo: 3}); got {
		t.Error("clear with no file must drop every mark")
	}
}

func TestSteerReloadSourcesAndMarkLifetime(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m, hc := m.applySteer(markCmd("r-0", "info", 1, 2))
	runSteerCmd(t, hc)

	m, cmd := m.applySteer(steer.Command{ID: "r-1", Cmd: "reload", Sources: []string{"notes"}, Wait: true})
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(dir, "r-1", time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true", r, ok)
	}
	v := &diffView{noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}}
	if _, got := m.attnMarkFor(v, textdiff.Row{RightNo: 1}); got {
		t.Error("an EXPLICIT reload must drop the attention marks (the interval refresh must not)")
	}

	m2, bad := m.applySteer(steer.Command{ID: "r-2", Cmd: "reload", Sources: []string{"weather"}, Wait: true})
	runSteerCmd(t, bad)
	if rr, _ := steer.AwaitReply(dir, "r-2", time.Second); rr.OK || !strings.Contains(rr.Error, "weather") {
		t.Errorf("reply = %+v, want ok:false naming the unknown source", rr)
	}
	_ = m2
}

// The other half of the lifetime rule: the background lane's own refresh — a
// status read landing on the Update loop, which rebuilds the working-tree
// diff — must leave the marks exactly where the agent put them. Nobody asked
// for them to go.
func TestAttentionMarksSurviveTheIntervalRefresh(t *testing.T) {
	t.Parallel()
	m, _ := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m, hc := m.applySteer(markCmd("i-1", "warn", 4, 6))
	runSteerCmd(t, hc)

	// Exactly what an interval (non-manual) status read lands as.
	nm, _ := m.Update(dataAvailableMsg{
		source: srcStatus,
		gen:    m.srcGen[srcStatus],
		value:  statusPayload{status: model.WorkingTreeStatus{}},
	})
	m = nm.(Model)

	v := &diffView{noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}}
	if _, got := m.attnMarkFor(v, textdiff.Row{RightNo: 5}); !got {
		t.Error("the interval auto-refresh wiped an attention mark no agent asked to clear")
	}
}

func TestSteerReloadDefaultsToNotes(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m, cmd := m.applySteer(steer.Command{ID: "r-3", Cmd: "reload", Wait: true})
	runSteerCmd(t, cmd)
	r, _ := steer.AwaitReply(dir, "r-3", time.Second)
	if !r.OK || !strings.Contains(r.Detail, "notes") {
		t.Fatalf("reply = %+v, want ok:true naming notes", r)
	}
	_ = m
}

func TestSteerPostsATransientNotice(t *testing.T) {
	t.Parallel()
	m, _ := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m, cmd := m.applySteer(steer.Command{ID: "n-9", Cmd: "reload", Sources: []string{"notes"}})
	runSteerCmd(t, cmd)
	if m.statusMsg == "" {
		t.Fatal("a reload with no diff open must post a status-bar notice")
	}
	withDiff := m.pushLayer(&diffView{})
	withDiff, cmd2 := withDiff.applySteer(steer.Command{ID: "n-10", Cmd: "reload", Sources: []string{"notes"}})
	runSteerCmd(t, cmd2)
	if withDiff.diffNotice == "" {
		t.Error("with a diff open the notice belongs on the diff notice line")
	}
}

func TestFileStateProtoCoversEveryTargetState(t *testing.T) {
	t.Parallel()
	want := map[model.FileState]string{
		model.StateUnstaged:  "unstaged",
		model.StateStaged:    "staged",
		model.StateUntracked: "untracked",
		model.StateCommitted: "commit",
	}
	for st, s := range want {
		if got := fileStateProto(st); got != s {
			t.Errorf("fileStateProto(%v) = %q, want %q", st, got, s)
		}
	}
}
