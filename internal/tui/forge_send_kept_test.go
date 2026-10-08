package tui

import (
	"errors"
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
		sendGroupsMsg{gen: old, pr: 7, groups: []domain.SendGroup{{ID: domain.GroupMine, Count: 1}, {ID: "review:r1", Count: 1}}},
		sendBodyMsg{gen: old, pr: 7, group: "review:r1", body: "x"},
	} {
		nm, cmd := m.Update(msg)
		mm := nm.(Model)
		if cmd != nil || mm.modal != nil || mm.running || layerOf[*sendReviewPopup](mm) != nil {
			t.Fatalf("%T acted in the new repo", msg)
		}
	}
}

// Item 17: a plan that fails after the body popup closed keeps the text; the
// next Verdict… of the same PR starts from it; a send that changed GitHub
// drops it.
func TestAFailedPlanKeepsTheTypedBody(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m, _ = m.openVerdict(7)
	p := layerOf[*sendReviewPopup](m)
	p.body = newTextField("a long thought-out verdict")
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	nm, _ := m.Update(forgeSendReadyMsg{gen: m.forgeGen, req: domain.PRSendRequest{PR: 7, Verdict: true, BodySet: true},
		err: errors.New("network down")})
	m = nm.(Model)
	m, _ = m.openVerdict(7)
	if got := layerOf[*sendReviewPopup](m).body.Value(); got != "a long thought-out verdict" {
		t.Fatalf("body = %q", got)
	}
	m = m.popLayer()
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7}, engine.Result{Changed: true}, nil)
	m, _ = m.openVerdict(7)
	if got := layerOf[*sendReviewPopup](m).body.Value(); got != "" {
		t.Fatalf("a sent body came back: %q", got)
	}
}
