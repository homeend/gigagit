package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// panelCands: one AI review (two remarks, one skipped), my notes (one), one
// draft reply — the spec's §5.3 picture.
func panelCands() domain.SendCandidates {
	t0 := time.Date(2026, 10, 9, 14, 2, 0, 0, time.UTC)
	return domain.SendCandidates{PR: 42, Head: "abc", Groups: []domain.SendCandidateGroup{
		{ID: "review:r1", Kind: "review", Agent: "Claude Code", Title: "two nits", Created: t0, Slot: 2, Rows: []domain.SendCandidate{
			{ID: "review:r1:1", Kind: "remark", Severity: "high", Path: "internal/git/pull.go", Range: [2]int{120, 134}, Side: "new", Summary: "lock released twice", Code: []string{"a", "b"}},
			{ID: "review:r1:2", Kind: "remark", Severity: "low", Path: "internal/tui/x.go", Range: [2]int{5, 5}, Side: "new", Summary: "nit", Skip: domain.SkipLinesChanged},
		}},
		{ID: domain.GroupMine, Kind: "mine", Slot: 1, Rows: []domain.SendCandidate{
			{ID: "n1", Kind: "note", Path: "README.md", Range: [2]int{12, 12}, Side: "new", Summary: "typo"},
		}},
		{ID: domain.GroupReplies, Kind: "replies", Rows: []domain.SendCandidate{
			{ID: "d1", Kind: "reply", Path: "internal/git/pull.go", Range: [2]int{120, 120}, Side: "new", Summary: "addressed"},
		}},
	}}
}

func panelModel(t *testing.T) (Model, *sendPanel) {
	t.Helper()
	m := prDiffModel(t)
	m, _ = m.handleSendPanel(sendPanelMsg{gen: m.forgeGen, pr: 7, cands: panelCands()})
	p := layerOf[*sendPanel](m)
	if p == nil {
		t.Fatal("no panel")
	}
	return m, p
}

// R7: nothing ticked on open; the title, the groups and every row show;
// a skip row shows its reason; the code excerpt sits under the current row.
func TestSendPanelOpensWithNothingTicked(t *testing.T) {
	t.Parallel()
	m, p := panelModel(t)
	if len(p.ticked) != 0 {
		t.Fatalf("ticked on open: %v", p.ticked)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Send to GitHub — #7", "0 ticked", "Review (AI)", "Claude Code", "My notes", "Draft replies",
		"[ ] high", "internal/git/pull.go:120-134", "lock released twice", "[ ] ~", "its lines changed", "README.md:12", "Body: none",
		"[space] tick", "[ctrl+s] send"} {
		if !strings.Contains(view, want) {
			t.Errorf("panel lacks %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "          a") { // the first remark is current: its code shows
		t.Errorf("no code excerpt under the current row:\n%s", view)
	}
}

// space ticks a row, never a skip row (the bottom bar says why); a toggles
// the group's sendable rows; the count follows.
func TestSendPanelTicks(t *testing.T) {
	t.Parallel()
	m, p := panelModel(t)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	if !p.ticked["review:r1:1"] || p.tickedCount() != 1 {
		t.Fatalf("space: %v", p.ticked)
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyDown}) // the skip row
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	if p.ticked["review:r1:2"] || !strings.Contains(p.notice, "its lines changed") {
		t.Fatalf("a skip row ticked / notice %q", p.notice)
	}
	m, _ = p.update(m, key("a")) // all/none in the review group: its one sendable row is ticked → none
	if p.tickedCount() != 0 {
		t.Fatalf("a with all ticked must untick: %v", p.ticked)
	}
	m, _ = p.update(m, key("a"))
	if !p.ticked["review:r1:1"] || p.ticked["review:r1:2"] || p.tickedCount() != 1 {
		t.Fatalf("a again ticks every sendable row of the group: %v", p.ticked)
	}
	if !strings.Contains(ansi.Strip(m.View()), "1 ticked") {
		t.Fatal("count")
	}
}

// b cycles none → each ticked AI review's text (newest first) → typed → none;
// with no AI review ticked, none → typed. e opens the body box prefilled.
func TestSendPanelBodyCycle(t *testing.T) {
	t.Parallel()
	m, p := panelModel(t)
	m, _ = p.update(m, key("b"))
	if p.bodyLabel() != "typed" {
		t.Fatalf("no review ticked: none → %q, want typed", p.bodyLabel())
	}
	m, _ = p.update(m, key("b"))
	if p.bodyLabel() != "none" {
		t.Fatalf("→ %q, want none", p.bodyLabel())
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace}) // tick the review's remark
	m, _ = p.update(m, key("b"))
	if !strings.HasPrefix(p.bodyLabel(), "review text (Claude Code") {
		t.Fatalf("→ %q, want the review's text", p.bodyLabel())
	}
	m, cmd := p.update(m, key("e")) // a review body: its text is read off-thread (none stored here: an empty box)
	m = drainCmds(t, m, cmd)
	box := layerOf[*sendPanelBody](m)
	if box == nil {
		t.Fatal("e opens the body box")
	}
	box.body = newTextField("my own words")
	m, _ = box.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if p.bodyLabel() != "typed" || p.typed != "my own words" {
		t.Fatalf("after e: %q / %q", p.bodyLabel(), p.typed)
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace}) // untick the review: typed stays
	if p.bodyLabel() != "typed" {
		t.Fatalf("typed survives an untick: %q", p.bodyLabel())
	}
	// A review body falls back to none when its review is unticked.
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	p.body, p.bodyFrom = bodyReview, "r1"
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	if p.bodyLabel() != "none" {
		t.Fatalf("a review body without its review: %q", p.bodyLabel())
	}
}

// ctrl+s: nothing ticked says so; else one Notes request with Verdict and
// the chosen body, handed to forgeSendCmd (the panel stays until the op
// reports, A7).
func TestSendPanelRequest(t *testing.T) {
	t.Parallel()
	m, p := panelModel(t)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if !strings.Contains(p.notice, "tick something to send") || layerOf[*sendPanel](m) == nil {
		t.Fatalf("notice %q", p.notice)
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	for i := 0; i < 4; i++ { // skip row, My notes header, its note, Draft replies header
		m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyDown}) // the draft reply
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	m, _ = p.update(m, key("b")) // review text
	req, ok := p.request()
	if !ok || req.PR != 7 || !req.Verdict || strings.Join(req.Notes, ",") != "review:r1:1,d1" || req.BodyFrom != "r1" || req.BodySet {
		t.Fatalf("req = %+v ok %v", req, ok)
	}
	m, _ = p.update(m, key("b"))
	p.typed = "typed words"
	req, _ = p.request()
	if req.BodyFrom != "" || !req.BodySet || req.Body != "typed words" {
		t.Fatalf("typed req = %+v", req)
	}
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil || layerOf[*sendPanel](m) == nil || !strings.Contains(m.statusMsg, "preparing the send to #7") {
		t.Fatalf("ctrl+s: cmd %v panel %v status %q", cmd != nil, layerOf[*sendPanel](m) != nil, m.statusMsg)
	}
	if k := m.keptSendBody; k == nil || k.group != sendGroupPanel || k.text != "typed words" {
		t.Fatalf("kept = %+v", m.keptSendBody)
	}
}

// A7: the panel closes when the send changed something; an abort (nothing
// changed) leaves it with its ticks.
func TestSendPanelClosesOnAChangeStaysOnAbort(t *testing.T) {
	t.Parallel()
	m, p := panelModel(t)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	fs := &forgeSendState{pr: 7, panel: true, req: domain.PRSendRequest{PR: 7, Notes: []string{"review:r1:1"}}}
	m2, _ := m.forgeSendFinished(fs, engine.Result{}, nil) // aborted
	if q := layerOf[*sendPanel](m2); q == nil || !q.ticked["review:r1:1"] {
		t.Fatal("an abort must leave the panel as it was")
	}
	m3, _ := m.forgeSendFinished(fs, engine.Result{Changed: true}, nil)
	if layerOf[*sendPanel](m3) != nil {
		t.Fatal("a change closes the panel")
	}
}

// enter opens the row's file in the PR diff above the panel, landing on
// its thread; esc on the diff returns to the panel with the ticks kept.
// Serial: env (prSendModel).
func TestSendPanelEnterOpensTheDiffAndReturns(t *testing.T) {
	m, _, head := prSendModel(t)
	id := addTUINote(t, m, head, 5, "look here")
	m = openPR7(t, m)
	m, cmd := m.openSendPanel(7)
	m = drainCmds(t, m, cmd)
	p := layerOf[*sendPanel](m)
	if p == nil || len(p.cands.Groups) != 1 || p.cands.Groups[0].Rows[0].ID != id {
		t.Fatalf("panel = %+v", p)
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	m, cmd = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainCmds(t, m, cmd)
	v := m.diffLayer()
	if v == nil || !v.cursorOnNote() {
		t.Fatalf("enter: diff %v on note %v", v != nil, v != nil && v.cursorOnNote())
	}
	m, _ = v.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if q := layerOf[*sendPanel](m); q == nil || !q.ticked[id] || m.diffLayer() != nil {
		t.Fatal("esc returns to the panel with its ticks")
	}
}

// Review Focus 1: opened from the PR tab's details with the PR's files not
// open, enter says so instead of opening a diff of the wrong view.
// Serial: env (prSendModel) — the real read path, the PR's file list
// closed again before the panel opens.
func TestSendPanelEnterNeedsTheOpenPR(t *testing.T) {
	m, _, head := prSendModel(t)
	addTUINote(t, m, head, 5, "look here")
	m = openPR7(t, m)
	m = m.closeFilesView()
	m, cmd := m.openSendPanel(7)
	m = drainCmds(t, m, cmd)
	p := layerOf[*sendPanel](m)
	if p == nil {
		t.Fatal("no panel")
	}
	m, cmd = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.diffLayer() != nil || !strings.Contains(p.notice, "open the pull request") {
		t.Fatalf("notice %q", p.notice)
	}
}

// No candidates: the panel does not open, the status says so (spec §5.1).
func TestSendPanelNothingToSend(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m, _ = m.handleSendPanel(sendPanelMsg{gen: m.forgeGen, pr: 7, cands: domain.SendCandidates{PR: 7}})
	if layerOf[*sendPanel](m) != nil || !strings.Contains(m.statusMsg, "nothing to send to #7") {
		t.Fatalf("status %q", m.statusMsg)
	}
}

// A prose review's group (empty Title) still has a readable header.
func TestSendPanelGroupHeaderWithoutATitle(t *testing.T) {
	t.Parallel()
	c := panelCands()
	c.Groups[0].Title = ""
	m := prDiffModel(t)
	m, _ = m.handleSendPanel(sendPanelMsg{gen: m.forgeGen, pr: 7, cands: c})
	if !strings.Contains(ansi.Strip(m.View()), "Review (AI) · Claude Code · ") {
		t.Fatal("header")
	}
}

// gotoNote lands on the thread holding a REPLY id too (the panel's draft rows).
func TestGotoNoteByReplyID(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.svc.UseNotesDir(t.TempDir())
	root, err := m.svc.NoteAdd(context.Background(), model.Note{
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: m.currentWorktree, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{18, 18}, Summary: "root"})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := m.svc.NoteReply(context.Background(), root.ID, model.Note{Summary: "reply"})
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.applySteer(steer.Command{ID: "g-1", Cmd: "navigate", File: "a.txt", Target: &steer.Target{State: "unstaged"}, Line: &steer.Line{Side: "new", No: 1}, Wait: true})
	m = pumpDiff(t, m, cmd)
	m, ok := m.gotoNote(rep.ID)
	if !ok || !m.diffLayer().cursorOnNote() {
		t.Fatal("gotoNote by a reply id")
	}
}

// Review Focus 2: a candidate deleted after the panel opened is skipped by
// the planner; the plan is empty, the error is said, the panel stays.
// Serial: env (prSendModel).
func TestSendPanelSurvivesAGoneCandidate(t *testing.T) {
	m, _, head := prSendModel(t)
	id := addTUINote(t, m, head, 5, "look here")
	m = openPR7(t, m)
	m, cmd := m.openSendPanel(7)
	m = drainCmds(t, m, cmd)
	p := layerOf[*sendPanel](m)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	if err := m.svc.NoteRemove(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	m, cmd = p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	nm, _ := m.Update(cmd()) // forgeSendReadyMsg: nothing to send → said in the panel, nothing runs
	m = nm.(Model)
	if q := layerOf[*sendPanel](m); q == nil || !strings.Contains(q.notice, "send: ") || q.planning {
		t.Fatalf("panel %v notice %q", layerOf[*sendPanel](m) != nil, p.notice)
	}
}

// Review finding 1: enter with a diff already open BELOW the panel (the
// panel's main entry is the PR diff's . menu) must still show the row's
// diff on top; esc returns to the panel. Serial: env (prSendModel).
func TestSendPanelEnterOverAnOpenDiff(t *testing.T) {
	m, _, head := prSendModel(t)
	id := addTUINote(t, m, head, 5, "look here")
	m = openPR7(t, m)
	l, _ := filesLine(t, m, "big.go")
	u, cmd := m.openDiffForFileLine(l)
	m = drainCmds(t, u.(Model), cmd)
	if m.diffLayer() == nil {
		t.Fatal("no PR diff")
	}
	m, cmd = m.openSendPanel(7)
	m = drainCmds(t, m, cmd)
	p := layerOf[*sendPanel](m)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	m, cmd = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainCmds(t, m, cmd)
	v, isDiff := m.topLayer().(*diffView)
	if !isDiff || !v.cursorOnNote() {
		t.Fatalf("top %T on note %v", m.topLayer(), isDiff && v.cursorOnNote())
	}
	m, _ = v.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if q, ok := m.topLayer().(*sendPanel); !ok || !q.ticked[id] {
		t.Fatalf("esc: top %T", m.topLayer())
	}
}

// Review finding 3: a repository switch drops the panel (its candidates
// and PR are the old repository's); a stale panel refuses ctrl+s.
func TestSendPanelDoesNotSurviveARepoSwitch(t *testing.T) {
	t.Parallel()
	m, p := panelModel(t)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	m.forgeGen++ // what reRoot does, among other things
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd != nil || !strings.Contains(p.notice, "repository changed") {
		t.Fatalf("stale ctrl+s: cmd %v notice %q", cmd != nil, p.notice)
	}
	m2, p2 := panelModel(t)
	m2, _ = p2.update(m2, key("e"))
	nm, _ := m2.reRoot(t.TempDir())
	m2 = nm.(Model)
	if layerOf[*sendPanel](m2) != nil || layerOf[*sendPanelBody](m2) != nil {
		t.Fatal("reRoot left the panel (or its body box) on the stack")
	}
}

// Review finding 4 (spec §5.3): e prefills the box with the CURRENT body —
// a chosen review's text, not an empty box. Serial: env (prSendModel).
func TestSendPanelEditPrefillsTheReviewText(t *testing.T) {
	m, _, head := prSendModel(t)
	addTUINote(t, m, head, 5, "look here")
	savePRReviewTUI(t, m, `{"version":1,"summary":"## Summary\ntwo nits","files":[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"name it"}]}]}`)
	m = openPR7(t, m)
	m, cmd := m.openSendPanel(7)
	m = drainCmds(t, m, cmd)
	p := layerOf[*sendPanel](m)
	if p == nil || p.cands.Groups[0].Kind != "review" {
		t.Fatalf("panel %+v", p)
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace}) // the remark
	m, _ = p.update(m, key("b"))                       // review text
	m, cmd = p.update(m, key("e"))
	m = drainCmds(t, m, cmd)
	box := layerOf[*sendPanelBody](m)
	if box == nil || !strings.Contains(box.body.Value(), "two nits") {
		t.Fatalf("box %v value %q", box != nil, box.body.Value())
	}
}

// Review findings 5 and 6: esc keeps nothing when nothing was typed (the
// verdict box's kept text survives), and a reopened panel with kept text
// starts with the body "typed", not "none".
func TestSendPanelKeptTextRules(t *testing.T) {
	t.Parallel()
	m, p := panelModel(t)
	m.keptSendBody = &keptSendBody{pr: 7, verdict: true, text: "my verdict"}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if got, ok := m.keptBodyFor(7, "", true); !ok || got != "my verdict" {
		t.Fatalf("the verdict's kept text went: %q %v", got, ok)
	}
	m.keptSendBody = &keptSendBody{pr: 7, group: sendGroupPanel, text: "typed before"}
	m, _ = m.handleSendPanel(sendPanelMsg{gen: m.forgeGen, pr: 7, cands: panelCands()})
	p = layerOf[*sendPanel](m)
	if p.typed != "typed before" || p.bodyLabel() != "typed" {
		t.Fatalf("reopened: typed %q body %q", p.typed, p.bodyLabel())
	}
}

// Only the panel's OWN send closes it: a one-note send from the diff's menu
// while the panel waits below leaves the panel with its ticks.
func TestSendPanelSurvivesAnotherSendOfThePR(t *testing.T) {
	t.Parallel()
	m, p := panelModel(t)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	other := &forgeSendState{pr: 7, req: domain.PRSendRequest{PR: 7, Notes: []string{"n9"}}}
	m2, _ := m.forgeSendFinished(other, engine.Result{Changed: true}, nil)
	if q := layerOf[*sendPanel](m2); q == nil || !q.ticked["review:r1:1"] {
		t.Fatal("a send from elsewhere must leave the panel as it was")
	}
	own := &forgeSendState{pr: 7, panel: true, req: domain.PRSendRequest{PR: 7, Notes: []string{"review:r1:1"}}}
	if m3, _ := m.forgeSendFinished(own, engine.Result{Changed: true}, nil); layerOf[*sendPanel](m3) != nil {
		t.Fatal("the panel's own send closes it")
	}
}

// ctrl+s while the plan is still being prepared does not start a second
// plan; the word goes to the panel's notice, and clears when the plan
// arrives.
func TestSendPanelCtrlSOnceWhilePlanning(t *testing.T) {
	t.Parallel()
	m, p := panelModel(t)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil || !p.planning || !strings.Contains(p.notice, "preparing the send to #7") {
		t.Fatalf("first ctrl+s: cmd %v planning %v notice %q", cmd != nil, p.planning, p.notice)
	}
	if _, again := p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS}); again != nil {
		t.Fatal("a second ctrl+s while planning must not start a second plan")
	}
	m, _ = m.handleForgeSendReady(forgeSendReadyMsg{gen: m.forgeGen, panel: true, err: errors.New("boom")})
	if p.planning || !strings.Contains(p.notice, "boom") {
		t.Fatalf("after the plan failed: planning %v notice %q", p.planning, p.notice)
	}
}
