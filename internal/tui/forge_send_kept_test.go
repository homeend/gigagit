package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// Item 11 / Review Focus 5: a plan, a group list or a body read that lands
// after a repo switch does nothing in the new repo.
func TestSendAnswersFromBeforeASwitchAreDropped(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	old := m.forgeGen
	m.forgeGen++ // what reRoot does
	for _, msg := range []tea.Msg{
		forgeSendReadyMsg{gen: old, req: domain.PRSendRequest{PR: 7, Mine: true}, op: engine.SendToForge{}},
		sendPanelMsg{gen: old, pr: 7, cands: panelCands()},
	} {
		nm, cmd := m.Update(msg)
		mm := nm.(Model)
		if cmd != nil || mm.modal != nil || mm.running || layerOf[*verdictPopup](mm) != nil || layerOf[*sendPanel](mm) != nil {
			t.Fatalf("%T acted in the new repo", msg)
		}
	}
}

// Item 17: a plan that fails after the body popup closed keeps the text; the
// next Verdict… of the same PR starts from it; its own send that changed
// GitHub drops it (C8).
func TestAFailedPlanKeepsTheTypedBody(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m, _ = m.openVerdict(7)
	p := layerOf[*verdictPopup](m)
	p.body = newTextField("a long thought-out verdict")
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	nm, _ := m.Update(forgeSendReadyMsg{gen: m.forgeGen, req: domain.PRSendRequest{PR: 7, Verdict: true, BodySet: true},
		err: errors.New("network down")})
	m = nm.(Model)
	m, _ = m.openVerdict(7)
	if got := layerOf[*verdictPopup](m).body.Value(); got != "a long thought-out verdict" {
		t.Fatalf("body = %q", got)
	}
	m = m.popLayer()
	sent := domain.PRSendRequest{PR: 7, Verdict: true, Body: "a long thought-out verdict", BodySet: true}
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7, req: sent}, engine.Result{Changed: true}, nil)
	m, _ = m.openVerdict(7)
	if got := layerOf[*verdictPopup](m).body.Value(); got != "" {
		t.Fatalf("a sent body came back: %q", got)
	}
}

// F-a: a send that did not come from the kept body's box — a resolve on the
// same PR, a send on another PR — leaves the typed text; its own send clears it.
func TestOnlyItsOwnSendClearsTheKeptBody(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.keptSendBody = &keptSendBody{pr: 7, verdict: true, text: "keep me"}
	resolve := domain.PRSendRequest{PR: 7, Resolve: []string{"PRRT_1"}}
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7, req: resolve}, engine.Result{Changed: true}, nil)
	other := domain.PRSendRequest{PR: 8, Verdict: true, BodySet: true}
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 8, req: other}, engine.Result{Changed: true}, nil)
	if m.keptSendBody == nil {
		t.Fatal("another send dropped the typed verdict body")
	}
	own := domain.PRSendRequest{PR: 7, Verdict: true, Body: "keep me", BodySet: true}
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7, req: own}, engine.Result{Changed: true}, nil)
	if m.keptSendBody != nil {
		t.Fatal("its own send did not clear it")
	}
}

// F-i: an aborted send of its own box keeps the text (nothing reached GitHub).
func TestAnAbortKeepsTheKeptBody(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.keptSendBody = &keptSendBody{pr: 7, verdict: true, text: "keep me"}
	own := domain.PRSendRequest{PR: 7, Verdict: true, Body: "keep me", BodySet: true}
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7, req: own}, engine.Result{}, nil)
	if m.keptSendBody == nil {
		t.Fatal("an abort dropped the typed body")
	}
}

// F-b: a failed resolve says nothing about kept text.
func TestAFailedResolveDoesNotSayTheTextIsKept(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.keptSendBody = &keptSendBody{pr: 7, verdict: true, text: "keep me"}
	m, _ = m.handleForgeSendReady(forgeSendReadyMsg{gen: m.forgeGen, req: domain.PRSendRequest{PR: 7, Resolve: []string{"x"}}, err: errors.New("boom")})
	if strings.Contains(m.statusMsg, "kept") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

// Plan 1 Task 7: a review posted whose draft replies then failed comes back
// as (Changed, err) with no Done for the replies. It is still the user's own
// change: the PR is re-read as "my send" (not "updated") and the typed body,
// which reached GitHub, is dropped.
func TestAPostedReviewWithFailedRepliesIsStillMyChange(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.prReadSeq = 3
	sent := domain.PRSendRequest{PR: 7, Notes: []string{"n1"}, Verdict: true, Body: "typed", BodySet: true}
	m.keptSendBody = &keptSendBody{pr: 7, group: sendGroupPanel, text: "typed"}
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7, req: sent}, engine.Result{Changed: true}, errors.New("the review was posted; replies: network down"))
	if m.prOwnSend != 7 || m.prOwnSendSeq != 3 {
		t.Fatalf("own send not recorded: pr %d seq %d", m.prOwnSend, m.prOwnSendSeq)
	}
	if m.keptSendBody != nil {
		t.Fatal("the body reached GitHub: nothing to keep")
	}
}

// A8: the panel's typed body is kept under its own key, per PR; the verdict
// box keeps its own; neither claims the other's text.
func TestPanelAndVerdictKeepSeparateBodies(t *testing.T) {
	t.Parallel()
	var m Model
	m.keptSendBody = &keptSendBody{pr: 7, group: sendGroupPanel, text: "panel text"}
	if got, ok := m.keptBodyFor(7, sendGroupPanel, false); !ok || got != "panel text" {
		t.Fatalf("panel body = %q %v", got, ok)
	}
	if _, ok := m.keptBodyFor(7, "", true); ok {
		t.Fatal("the verdict box must not see the panel's text")
	}
	if _, ok := m.keptBodyFor(8, sendGroupPanel, false); ok {
		t.Fatal("another PR must not see it")
	}
	panelReq := domain.PRSendRequest{PR: 7, Notes: []string{"n1"}, Verdict: true, Body: "panel text", BodySet: true}
	if !m.keptSendBody.from(panelReq) {
		t.Fatal("the panel's own send clears its kept body")
	}
	if m.keptSendBody.from(domain.PRSendRequest{PR: 7, Verdict: true, Body: "x", BodySet: true}) {
		t.Fatal("a verdict-only send is not the panel's")
	}
}
