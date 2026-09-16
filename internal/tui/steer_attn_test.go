package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

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

	v := &diffView{noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}}

	// A `notes` reload is what EVERY note mutation auto-posts (gg note add, gg
	// review --notes, the MCP note tools), so it is NOT the agent saying "look
	// again": it must leave the bands alone, or the documented
	// highlight-then-note flow would wipe the band it just painted. Notes do
	// not rebuild the diff geometry the ranges are anchored against.
	m, cmd := m.applySteer(steer.Command{ID: "r-1", Cmd: "reload", Sources: []string{"notes"}, Wait: true})
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(dir, "r-1", time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true", r, ok)
	}
	if _, got := m.attnMarkFor(v, textdiff.Row{RightNo: 1}); !got {
		t.Error("a notes-only reload must KEEP the attention marks — every note mutation auto-posts one")
	}

	// `status` DOES rebuild the working-tree diff the ranges anchor against,
	// so it drops them: a band whose range drifted under an edit is the
	// agent's to re-post.
	m, cmd = m.applySteer(steer.Command{ID: "r-1b", Cmd: "reload", Sources: []string{"status"}, Wait: true})
	runSteerCmd(t, cmd)
	if rr, ok := steer.AwaitReply(dir, "r-1b", time.Second); !ok || !rr.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true", rr, ok)
	}
	if _, got := m.attnMarkFor(v, textdiff.Row{RightNo: 1}); got {
		t.Error("a status reload must drop the attention marks")
	}

	// …and so does `all`, which contains status. The cmd is deliberately not
	// run: reloadAllCmd fans out over every source, and the clear itself is
	// synchronous on the returned Model.
	m2, hc2 := m.applySteer(markCmd("r-0b", "info", 1, 2))
	runSteerCmd(t, hc2)
	m2, _ = m2.applySteer(steer.Command{ID: "r-1c", Cmd: "reload", Sources: []string{"all"}})
	if _, got := m2.attnMarkFor(v, textdiff.Row{RightNo: 1}); got {
		t.Error("a reload all must drop the attention marks")
	}

	m3, bad := m.applySteer(steer.Command{ID: "r-2", Cmd: "reload", Sources: []string{"weather"}, Wait: true})
	runSteerCmd(t, bad)
	if rr, _ := steer.AwaitReply(dir, "r-2", time.Second); rr.OK || !strings.Contains(rr.Error, "weather") {
		t.Errorf("reply = %+v, want ok:false naming the unknown source", rr)
	}
	_ = m3
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

// attnBandRows is cursorRows with one row turned into a pure Add (its left
// side is a gap), so a band's dotted gap filler is covered alongside its hot
// cells — the two the cursor's own helpers used to hijack.
func attnBandRows(n, changed, add int) []textdiff.Row {
	rows := cursorRows(n, changed)
	rows[add] = textdiff.Row{Kind: textdiff.Add, Right: "y", RightNo: add + 1}
	return rows
}

// TestAttentionBandPaintsChangedAndGapRows: an agent marks CHANGED lines, so
// the band has to reach the add/del cells and the gap filler — the two places
// cellMark.row means "cursor". The tone (warn = 94) must replace the hot
// shades (52/22) there and must never be confused with the cursor's brighter
// variants (88/28) or its grey band (237). The real cursor still outranks it.
func TestAttentionBandPaintsChangedAndGapRows(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	m := openedDiffModel(12, attnBandRows(40, 20, 22), []int{20})
	m.width = 80
	v := m.diffLayer()
	v.noteAddr = model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}
	v.offset = 20 // rows 20..22 are the first three rendered lines
	m.attention = map[attentionKey][]steerMark{
		{path: "a.txt", state: "unstaged"}: {{side: "new", start: 21, end: 23, tone: "warn"}},
	}

	lines := m.diffPaneLines(v, 80, 10, 0, 0, "row") // no cursor in range
	changed, gap := lines[0], lines[2]
	if !strings.Contains(changed, "48;5;94") {
		t.Errorf("a marked Changed row must wear the warn band (94): %q", changed)
	}
	for _, bad := range []struct{ code, what string }{
		{"48;5;88", "the cursor del shade"},
		{"48;5;28", "the cursor add shade"},
		{"48;5;237", "the cursor grey band"},
		{"48;5;52", "the plain del shade"},
		{"48;5;22", "the plain add shade"},
	} {
		if strings.Contains(changed, bad.code) {
			t.Errorf("a marked Changed row must not wear %s (%s): %q", bad.what, bad.code, changed)
		}
	}
	if !strings.Contains(gap, "48;5;94") {
		t.Errorf("a marked Add row's gap filler must wear the warn band (94): %q", gap)
	}
	if strings.Contains(gap, "48;5;237") {
		t.Errorf("a marked Add row's gap must not wear the cursor gap background (237): %q", gap)
	}
	// An unmarked row in the same frame is untouched.
	if plain := lines[4]; strings.Contains(plain, "48;5;94") {
		t.Errorf("an unmarked row wore the band: %q", plain)
	}
	// The cursor outranks the band ON ITS OWN CELL: the user must always see
	// where they are. The cursor sits on ONE side (spec §4.7), so the other
	// cell keeps the band — asserted for both sides by flipping onOld.
	panes := func(onOld bool) []string {
		v.onOld = onOld
		row := m.diffPaneLines(v, 80, 10, 20, 21, "row")[0]
		p := strings.SplitN(row, "│", 2)
		if len(p) != 2 {
			t.Fatalf("the cursor row must have exactly one pane separator: %q", row)
		}
		return p
	}
	p := panes(false) // the default: the cursor is on the new (right) side
	if strings.Contains(p[1], "48;5;94") {
		t.Errorf("the cursor's own cell must outrank the band: %q", p[1])
	}
	if !strings.Contains(p[1], "48;5;28") {
		t.Errorf("the cursor cell on a marked Changed row must keep its 28 shade: %q", p[1])
	}
	if !strings.Contains(p[0], "48;5;94") {
		t.Errorf("the cell the cursor is not on must keep the band (94): %q", p[0])
	}
	p = panes(true) // mirrored on the old (left) side
	if strings.Contains(p[0], "48;5;94") {
		t.Errorf("the cursor's own cell must outrank the band: %q", p[0])
	}
	if !strings.Contains(p[0], "48;5;88") {
		t.Errorf("the cursor cell on a marked Changed row must keep its 88 shade: %q", p[0])
	}
	if !strings.Contains(p[1], "48;5;94") {
		t.Errorf("the cell the cursor is not on must keep the band (94): %q", p[1])
	}
	v.onOld = false
}

// TestSteerHighlightResolvesACommitTarget: the TUI must not depend on the
// producer sending a full sha. A commit-target mark is keyed on the FEED's
// hash — the string a commit diff's noteAddr carries — so a short hash lands
// on the open diff; a commit the feed does not hold is refused rather than
// stored under a key nothing can ever match.
func TestSteerHighlightResolvesACommitTarget(t *testing.T) {
	t.Parallel()
	m, hash := navFeedModel(t)
	dir := m.steerDir

	m, cmd := m.applySteer(steer.Command{
		ID: "hcm-1", Cmd: "highlight", File: "a.txt",
		Target: &steer.Target{State: "commit", Commit: hash[:7]},
		Side:   "new", Start: 3, End: 4, Tone: "info", Wait: true,
	})
	runSteerCmd(t, cmd)
	if r, ok := steer.AwaitReply(dir, "hcm-1", time.Second); !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true for a short-hash target", r, ok)
	}
	v := &diffView{noteAddr: model.FileAddress{State: model.StateCommitted, Commit: hash, Path: "a.txt"}}
	if _, got := m.attnMarkFor(v, textdiff.Row{RightNo: 3}); !got {
		t.Error("a short-hash mark must paint the diff whose address carries the full hash")
	}

	m2, bad := m.applySteer(steer.Command{
		ID: "hcm-2", Cmd: "highlight", File: "a.txt",
		Target: &steer.Target{State: "commit", Commit: "deadbee"},
		Side:   "new", Start: 1, End: 1, Tone: "info", Wait: true,
	})
	runSteerCmd(t, bad)
	r, _ := steer.AwaitReply(dir, "hcm-2", time.Second)
	if r.OK || !strings.Contains(r.Error, "commit not loaded in the feed") {
		t.Errorf("reply = %+v, want ok:false naming an unloaded commit", r)
	}
	_ = m2
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
