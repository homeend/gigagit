package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

// notedModel opens a diff over 40 rows with notes on lines 5 and 25. The
// stamped noteAddr matters: diffNoteAddress() reads it and every note key bails
// without one, and loadNotesCmd tags its result with m.diffTag.
func notedModel(t *testing.T) Model {
	t.Helper()
	m := openedDiffModel(12, cursorRows(40, 4, 24), []int{4, 24})
	v := m.diffLayer()
	v.title = "a/b.go"
	v.noteAddr = model.FileAddress{State: model.StateUnstaged, Worktree: "/wt", Path: "a/b.go"}
	m.diffTag = statusDiffTag("a/b.go", false)
	v.notes = []domain.ResolvedNote{
		rootNote("n1", 5, "first", "", model.NoteSourceUser, model.NoteActive),
		rootNote("n2", 25, "second", "", model.NoteSourceAgent, model.NoteActive),
	}
	v.relayout(0)
	return m
}

func TestBraceJumpsBetweenAnnotatedLines(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	v.setCursorLine(0, m.diffBodyRows())
	m, _ = v.update(m, synthKey("}"))
	if m.diffLayer().curLine != 4 {
		t.Fatalf("} from the top = line %d, want 4 (the note on line 5)", m.diffLayer().curLine)
	}
	m, _ = m.diffLayer().update(m, synthKey("}"))
	if m.diffLayer().curLine != 24 {
		t.Fatalf("second } = line %d, want 24", m.diffLayer().curLine)
	}
	m, _ = m.diffLayer().update(m, synthKey("{"))
	if m.diffLayer().curLine != 4 {
		t.Fatalf("{ = line %d, want 4", m.diffLayer().curLine)
	}
}

func TestBraceExpandsAFoldedNote(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 4, 34), []int{4, 34})
	v := m.diffLayer()
	v.notes = []domain.ResolvedNote{rootNote("n1", 21, "buried", "", model.NoteSourceUser, model.NoteActive)}
	v.partial = true
	v.rebuild()
	v.setCursorLine(0, m.diffBodyRows())
	m, _ = v.update(m, synthKey("}"))
	v = m.diffLayer()
	if v.partial {
		t.Fatal("} onto a folded note must expand the view (like f)")
	}
	if v.lines[v.curLine].Row.RightNo != 21 {
		t.Fatalf("cursor landed on RightNo %d, want 21", v.lines[v.curLine].Row.RightNo)
	}
}

func TestAgentLayerToggleHidesAgentNotes(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m, _ = m.diffLayer().update(m, synthKey("a"))
	if !m.notesAgentOff || !m.diffLayer().hideAgent {
		t.Fatal("a must flip BOTH the session flag and the view's mirror")
	}
	for _, dr := range m.diffLayer().disp {
		if dr.note != nil && strings.Contains(dr.note.text, "second") {
			t.Fatal("the agent note must be gone from the display stream")
		}
	}
}

func TestCOpensTheNotePopupAnchoredAtTheCursor(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.diffLayer().setCursorLine(9, m.diffBodyRows())
	m, _ = m.diffLayer().update(m, synthKey("c"))
	p, ok := m.topLayer().(*notePopup)
	if !ok {
		t.Fatalf("c must push a notePopup, top is %T", m.topLayer())
	}
	if p.mode != noteAdd || p.line != 10 || p.side != model.NoteSideNew {
		t.Fatalf("popup anchor = mode %v line %d side %q", p.mode, p.line, p.side)
	}
	// An empty summary cancels: esc-equivalent, nothing submitted.
	// synthKey maps only enter/esc/space to a Type — ctrl+s must be built by hand.
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if _, still := m.topLayer().(*notePopup); still {
		t.Fatal("ctrl+s with an empty summary must close the popup")
	}
	if cmd != nil {
		t.Fatal("ctrl+s with an empty summary must not dispatch a write")
	}
}

// E/R act only on a note NEXT to the cursor: anchored on the cursor line
// (rows just below it) or, failing that, on the real line just above (rows
// just above it). Further away the keys are inert.
func TestERTargetOnlyAdjacentNotes(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	body := m.diffBodyRows()
	m.diffLayer().setCursorLine(24, body) // n2 is anchored on line 25 = logical 24
	m, _ = m.diffLayer().update(m, synthKey("E"))
	p, ok := m.topLayer().(*notePopup)
	if !ok || p.mode != noteEdit || p.targetID != "n2" {
		t.Fatalf("E on the anchored line must edit n2, got %#v (ok %v)", p, ok)
	}
	m = m.popLayer()
	m.diffLayer().setCursorLine(25, body) // the line just below: n2's rows sit right above
	m, _ = m.diffLayer().update(m, synthKey("R"))
	p, _ = m.topLayer().(*notePopup)
	if p == nil || p.mode != noteReply || p.targetID != "n2" {
		t.Fatalf("R one line below the note must reply to n2, got %#v", p)
	}
	m = m.popLayer()
	m.diffLayer().setCursorLine(30, body) // out of reach
	m, _ = m.diffLayer().update(m, synthKey("E"))
	if _, ok := m.topLayer().(*notePopup); ok || m.modal != nil {
		t.Fatal("E away from every note must be inert")
	}
	if rows := m.noteMenuRows(); rows != nil {
		t.Fatalf("the . menu must offer no note rows away from every note, got %d", len(rows))
	}
}

// Two threads on one line: E/R/Delete raise a chooser listing them by
// summary; the pick runs the action, Cancel does nothing.
func TestSeveralNotesOnOneLineRaiseAChooser(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	v.notes = append(v.notes, rootNote("n3", 25, "third", "", model.NoteSourceUser, model.NoteActive))
	v.relayout(0)
	body := m.diffBodyRows()
	v.setCursorLine(24, body)
	if ts := m.notesAtCursor(); len(ts) != 2 || ts[0].note.ID != "n2" || ts[1].note.ID != "n3" {
		t.Fatalf("notesAtCursor = %+v, want n2 then n3", ts)
	}
	m, _ = m.diffLayer().update(m, synthKey("E"))
	if _, ok := m.topLayer().(*notePopup); ok || m.modal == nil || m.modal.req.ID != "note-choose" {
		t.Fatalf("E with two notes in reach must raise the chooser, modal=%v", m.modal)
	}
	opts := m.modal.req.Options
	if len(opts) != 3 || !strings.HasPrefix(opts[1], "2: third") || opts[2] != "Cancel" {
		t.Fatalf("chooser options = %q", opts)
	}
	nm, _ := m.modal.onResolve(m, opts[1])
	m = nm.(Model)
	p, ok := m.topLayer().(*notePopup)
	if !ok || p.targetID != "n3" {
		t.Fatalf("picking 2 must edit n3, got %#v", p)
	}
	m = m.popLayer()
	m.modal = nil
	nm, _ = m.withNoteTarget(func(m Model, tg noteTarget) (tea.Model, tea.Cmd) { t.Fatal("Cancel must not act"); return m, nil })
	m = nm.(Model)
	nm, _ = m.modal.onResolve(m, "Cancel")
	if _, ok := nm.(Model).topLayer().(*notePopup); ok {
		t.Fatal("Cancel opened a popup")
	}
}

// A note added on the bottom visible line must come into view: the loaded
// notes relayout scrolls just enough to show the new rows under the cursor.
func TestLoadedNotesUnderTheCursorAreRevealed(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	v := m.diffLayer()
	v.title = "a/b.go"
	v.noteAddr = model.FileAddress{State: model.StateUnstaged, Worktree: "/wt", Path: "a/b.go"}
	m.diffTag = statusDiffTag("a/b.go", false)
	body := m.diffBodyRows()
	v.setCursorLine(body-1, body) // the last visible line, offset 0
	if v.offset != 0 {
		t.Fatalf("precondition: offset %d", v.offset)
	}
	nm, _ := m.Update(notesLoadedMsg{tag: m.diffTag, notes: []domain.ResolvedNote{
		rootNote("n9", body, "on the last line", "why", model.NoteSourceUser, model.NoteActive),
	}})
	m = nm.(Model)
	v = m.diffLayer()
	start, end := v.lineStart[v.curLine], v.lineStart[v.curLine+1]
	if end-start != 3 {
		t.Fatalf("expected 3 display rows for the line (line + summary + rationale), got %d", end-start)
	}
	if end > v.offset+body {
		t.Fatalf("note rows end at %d but the viewport shows [%d,%d)", end, v.offset, v.offset+body)
	}
	if start < v.offset {
		t.Fatal("the cursor row scrolled out of view")
	}
}

func TestNoteKeysInertWithoutNotes(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	// An unstamped view makes diffNoteAddress bail before noteNearCursor is
	// ever consulted, which would make this test vacuous: give it the same
	// address notedModel has, so E/R really do reach the no-note path.
	m.diffLayer().title = "a/b.go"
	m.diffLayer().noteAddr = model.FileAddress{State: model.StateUnstaged, Worktree: "/wt", Path: "a/b.go"}
	m.diffTag = statusDiffTag("a/b.go", false)
	for _, k := range []string{"E", "R", "}", "{"} {
		mm, _ := m.diffLayer().update(m, synthKey(k))
		if _, isPopup := mm.topLayer().(*notePopup); isPopup {
			t.Fatalf("%s must be inert with no notes", k)
		}
	}
}

func TestSrcNotesRegistered(t *testing.T) {
	t.Parallel()
	if sourceNames[srcNotes] != "notes" || sourceDisplayName(srcNotes) != "notes" {
		t.Fatal("srcNotes needs a name and a display name")
	}
	if len(srcConsumers[srcNotes]) == 0 {
		t.Fatal("srcNotes must list its consumer panels")
	}
	for _, it := range scheduledItems {
		if !it.isFetch && !it.isRemoteTags && it.source == srcNotes {
			t.Fatal("srcNotes must never be polled by the background scheduler")
		}
	}
}

func TestNoteDeleteRowOnlyWithANoteInReach(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.diffLayer().setCursorLine(24, m.diffBodyRows())
	if _, ok := m.noteDeleteRow(); !ok {
		t.Fatal("the . menu must offer Delete note when a note sits next to the cursor")
	}
	m.diffLayer().setCursorLine(30, m.diffBodyRows())
	if _, ok := m.noteDeleteRow(); ok {
		t.Fatal("no Delete note row away from every note")
	}
	m2 := openedDiffModel(12, cursorRows(40), nil)
	if _, ok := m2.noteDeleteRow(); ok {
		t.Fatal("no notes ⇒ no Delete note row")
	}
}

var _ = tea.KeyMsg{}

// TestDiffLoadersStampTheNoteAddress pins the I1 fix: the address a note is
// stored with comes from the LOADER, which knows which two texts it is
// showing. Panel focus cannot tell a staged diff from an unstaged one, and the
// stored State is the pair the sweep re-reads — a staged note filed as
// StateUnstaged is resolved against the working file and silently deleted at
// the next start.
func TestDiffLoadersStampTheNoteAddress(t *testing.T) {
	t.Parallel()
	base := Model{width: 100, height: 30, currentWorktree: "/wt",
		svc: domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})}
	base.status.Branch = "main"
	mod := model.FileStatus{Path: "a.go", Staged: 'M', Unstaged: 'M'}
	unt := model.FileStatus{Path: "n.go", Unstaged: '?', Kind: model.KindUntracked}

	for _, tc := range []struct {
		name   string
		f      model.FileStatus
		staged bool
		want   model.FileState
	}{
		{"staged panel", mod, true, model.StateStaged},
		{"files panel", mod, false, model.StateUnstaged},
		{"files panel, untracked", unt, false, model.StateUntracked},
	} {
		// The view the enter handler puts on screen while the read is in flight…
		u, _ := base.openStatusDiff(tc.f, tc.staged)
		got, ok := u.(Model).diffNoteAddress()
		if !ok || got.State != tc.want || got.Path != tc.f.Path || got.Worktree != "/wt" {
			t.Fatalf("%s: loading-view address = %+v (ok %v), want state %v path %q", tc.name, got, ok, tc.want, tc.f.Path)
		}
		// …and the PRIVATE view the loader fills and the diffMsg installs.
		msg, isDiff := base.loadStatusDiffCmd(tc.f, tc.staged)().(diffMsg)
		if !isDiff || msg.view.noteAddr.State != tc.want || msg.view.noteAddr.Path != tc.f.Path {
			t.Fatalf("%s: loaded-view address = %+v", tc.name, msg.view.noteAddr)
		}
	}

	// A commit file diff is hash^ → hash, which is StateCommitted's own pair.
	msg, ok := base.loadCommitDiffCmd("deadbeef", contentLine{path: "a.go", status: "M"})().(diffMsg)
	if !ok || msg.view.noteAddr.State != model.StateCommitted ||
		msg.view.noteAddr.Commit != "deadbeef" || msg.view.noteAddr.Path != "a.go" {
		t.Fatalf("commit diff address = %+v", msg.view.noteAddr)
	}

	// A two-sided comparison names no single provenance, so it carries no
	// address at all and every note key is inert on it.
	cmp := base.pushLayer(&diffView{title: "a ↔ b", compare: true})
	if addr, ok := cmp.diffNoteAddress(); ok {
		t.Fatalf("a comparison must carry no note address, got %+v", addr)
	}
	if cmp.loadNotesCmd() != nil {
		t.Fatal("a comparison must not even read notes")
	}
}

// --- Fix round 1 -----------------------------------------------------------

// oldSideNote is rootNote's old-side twin: NotesFor sorts new-side FIRST, so a
// low old-side line legitimately precedes a high new-side line in v.notes.
func oldSideNote(id string, line int, summary string) domain.ResolvedNote {
	n := rootNote(id, line, summary, "", model.NoteSourceUser, model.NoteActive)
	n.Note.Side = model.NoteSideOld
	return n
}

// TestNoteTargetIsTheNearestLineNotTheLastListed: with notes on BOTH sides the
// list order (new-side first, then by line) puts the FARTHER note last, so
// picking "the last qualifying note" targets the wrong one.
func TestNoteTargetFollowsAdjacencyNotListOrder(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	v.notes = []domain.ResolvedNote{
		rootNote("new20", 20, "near", "", model.NoteSourceUser, model.NoteActive),
		oldSideNote("old3", 3, "far"),
	}
	v.relayout(0)
	body := m.diffBodyRows()
	v.setCursorLine(19, body) // new20's anchored line
	if tg, ok := m.noteNearCursor(); !ok || tg.note.ID != "new20" {
		t.Fatalf("target = %q (ok %v), want new20", tg.note.ID, ok)
	}
	v.setCursorLine(20, body) // one below: still adjacent (rows right above)
	if tg, ok := m.noteNearCursor(); !ok || tg.note.ID != "new20" {
		t.Fatalf("target one line below = %q (ok %v), want new20", tg.note.ID, ok)
	}
	v.setCursorLine(2, body) // old3's line (old side, list order last)
	if tg, ok := m.noteNearCursor(); !ok || tg.note.ID != "old3" {
		t.Fatalf("target on the old-side line = %q (ok %v), want old3", tg.note.ID, ok)
	}
	v.setCursorLine(30, body)
	if _, ok := m.noteNearCursor(); ok {
		t.Fatal("no note is adjacent to line 30")
	}
}

// TestAgentLayerTargetingMatchesRendering: with the agent layer off, a user
// reply under an agent root still RENDERS, so it must still be targetable —
// E edits that row's note, R replies into its (hidden) root.
func TestAgentLayerTargetingMatchesRendering(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	root := reply(rootNote("aroot", 5, "bot's root", "", model.NoteSourceAgent, model.NoteActive),
		"ureply", "ada", "my reply", model.NoteSourceUser)
	v.notes = []domain.ResolvedNote{root}
	v.hideAgent = true
	v.relayout(0)
	v.setCursorLine(4, m.diffBodyRows()) // the root's anchored line

	// The row is on screen…
	shown := false
	for _, dr := range v.disp {
		if dr.note != nil && strings.Contains(dr.note.text, "my reply") {
			shown = true
		}
	}
	if !shown {
		t.Fatal("fixture broken: the user reply must render with the agent layer off")
	}
	// …so it is what E targets.
	m2, _ := m.diffLayer().update(m, synthKey("E"))
	p, ok := m2.topLayer().(*notePopup)
	if !ok || p.targetID != "ureply" {
		t.Fatalf("E must edit the VISIBLE row's note, got %#v (ok %v)", p, ok)
	}
	m2 = m2.popLayer()
	m3, _ := m2.diffLayer().update(m2, synthKey("R"))
	p, ok = m3.topLayer().(*notePopup)
	if !ok || p.targetID != "aroot" {
		t.Fatalf("R must reply into the row's ROOT thread, got %#v (ok %v)", p, ok)
	}
	// A thread with nothing left to show is not targetable at all.
	v.notes = []domain.ResolvedNote{rootNote("only", 5, "bot", "", model.NoteSourceAgent, model.NoteActive)}
	v.relayout(0)
	if _, ok := m.noteNearCursor(); ok {
		t.Fatal("a fully hidden thread must not be targetable")
	}
}

// notedFileStepModel is treeDiffModel with c.go carrying notes (two plain file
// steps away) and no notes in the open diff, so }/{ fall straight through to
// the file step.
func notedFileStepModel() Model {
	m := treeDiffModel(1) // on a.go
	m.noteCounts = domain.NoteCounts{ByPath: map[string]int{"c.go": 1}}
	return m
}

// TestNoteFileStepArmDoesNotCrossTalkWithEnd: }/{ own their arm, so a primed
// note step is never performed by end/N and vice versa.
func TestNoteFileStepArmDoesNotCrossTalkWithEnd(t *testing.T) {
	t.Parallel()
	// } primes its own arm…
	m := notedFileStepModel()
	u, _ := m.Update(keyMsg("}"))
	mm := u.(Model)
	if mm.diffLayer().fileArm != fileArmNextNote {
		t.Fatalf("} must prime fileArmNextNote, got %d", mm.diffLayer().fileArm)
	}
	if !strings.Contains(fileArmCue(fileArmNextNote), "}") {
		t.Fatalf("the cue must name }, got %q", fileArmCue(fileArmNextNote))
	}
	// …and end must NOT perform it: it re-primes its own arm instead.
	u2, cmd := mm.Update(keyMsg("end"))
	mm2 := u2.(Model)
	if mm2.filesView.sel != 1 || cmd != nil {
		t.Fatalf("end must not step after } primed, sel=%d cmd=%v", mm2.filesView.sel, cmd)
	}
	if mm2.diffLayer().fileArm != fileArmNext {
		t.Fatalf("end after } must re-prime fileArmNext, got %d", mm2.diffLayer().fileArm)
	}

	// The other direction: end primes, } must not step the note walk.
	m = notedFileStepModel()
	u, _ = m.Update(keyMsg("end"))
	mm = u.(Model)
	if mm.diffLayer().fileArm != fileArmNext {
		t.Fatalf("end must prime fileArmNext, got %d", mm.diffLayer().fileArm)
	}
	u2, cmd = mm.Update(keyMsg("}"))
	mm2 = u2.(Model)
	if mm2.filesView.sel != 1 || cmd != nil {
		t.Fatalf("} must not step after end primed, sel=%d cmd=%v", mm2.filesView.sel, cmd)
	}
	if mm2.diffLayer().fileArm != fileArmNextNote {
		t.Fatalf("} after end must re-prime fileArmNextNote, got %d", mm2.diffLayer().fileArm)
	}
}

// TestNoteFileStepTwoPress: }} skips the note-less neighbour and lands on the
// next file that actually carries notes.
func TestNoteFileStepTwoPress(t *testing.T) {
	t.Parallel()
	m := notedFileStepModel()
	u, _ := m.Update(keyMsg("}"))
	u2, cmd := u.(Model).Update(keyMsg("}"))
	mm := u2.(Model)
	if mm.filesView.sel != 3 || mm.diffTag != "commit:abc:c.go" {
		t.Fatalf("}} must land on c.go, sel=%d tag=%q", mm.filesView.sel, mm.diffTag)
	}
	if cmd == nil {
		t.Fatal("stepping must return the loader cmd")
	}
}

// TestDiffHintFitsItsBudget pins the footer budget the note groups had to fit
// into: the widest variant must survive a 140-column terminal whole.
func TestDiffHintFitsItsBudget(t *testing.T) {
	t.Parallel()
	for _, mode := range []longMode{longScroll, longWrap, longTruncate} {
		h := diffHintFor(mode)
		if w := lipgloss.Width(h); w > 140 {
			t.Errorf("hint (mode %d) is %d columns, budget is 140: %q", mode, w, h)
		}
		for _, k := range []string{"[c/}{]", "[z]", "[e]", "[n/p]", "[f]", "[h/b]", "[esc] close"} {
			if !strings.Contains(h, k) {
				t.Errorf("hint (mode %d) lost %s: %q", mode, k, h)
			}
		}
	}
}

// TestNoteMenuRowsOfferEditReplyDelete: E/R are not in the footer, so the .
// menu must carry them next to Delete note — and Delete must confirm first.
func TestNoteMenuRowsOfferEditReplyDelete(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.diffLayer().setCursorLine(24, m.diffBodyRows()) // next to n2
	ids := map[string]actionRow{}
	for _, r := range m.noteMenuRows() {
		ids[r.id] = r
	}
	for _, want := range []string{"note-edit", "note-reply", "note-delete"} {
		if _, ok := ids[want]; !ok {
			t.Fatalf("the . menu must offer %s, got %v", want, ids)
		}
	}
	if strings.Contains(ids["note-edit"].label, "[") {
		t.Fatalf("menu labels carry no key hints: %q", ids["note-edit"].label)
	}
	// Edit opens the popup on the same target the key would.
	nm, _ := ids["note-edit"].run(m)
	if p, ok := nm.(Model).topLayer().(*notePopup); !ok || p.mode != noteEdit {
		t.Fatalf("the Edit note row must open the edit popup, top is %T", nm.(Model).topLayer())
	}
	// Delete confirms instead of writing straight away.
	nm, cmd := ids["note-delete"].run(m)
	dm := nm.(Model)
	if dm.modal == nil || dm.modal.req.ID != "note-remove" {
		t.Fatalf("Delete note must raise a confirmation, modal = %#v", dm.modal)
	}
	if cmd != nil {
		t.Fatal("Delete note must not write before the confirmation is answered")
	}
	if dm.modal.req.Options[len(dm.modal.req.Options)-1] != "Cancel" {
		t.Fatalf("esc must map to Cancel, options = %v", dm.modal.req.Options)
	}
	// Cancel writes nothing.
	nm, cmd = dm.resolveModal("Cancel")
	if cmd != nil || nm.(Model).modal != nil {
		t.Fatal("Cancel must close the modal and write nothing")
	}
	// With replies, the prompt says how many go with it.
	m2 := notedModel(t)
	v := m2.diffLayer()
	v.notes = []domain.ResolvedNote{reply(rootNote("r", 5, "root", "", model.NoteSourceUser, model.NoteActive),
		"c1", "ada", "reply", model.NoteSourceUser)}
	v.relayout(0)
	v.setCursorLine(4, m2.diffBodyRows()) // the root's anchored line
	row, _ := m2.noteDeleteRow()
	nm, _ = row.run(m2)
	if p := nm.(Model).modal.req.Prompt; !strings.Contains(p, "1") {
		t.Fatalf("the prompt must count the replies, got %q", p)
	}
}
