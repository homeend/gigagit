package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

func rewordModel(t *testing.T) Model {
	t.Helper()
	m := footerModel()
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "abc1234def", Subject: "old subject", Parents: []string{"p"}}}
	return m
}

func TestRewordPopupFallsBackToSubject(t *testing.T) {
	// No git log response wired: the full-message read yields nothing, so the
	// popup falls back to the row's subject.
	m := rewordModel(t)
	m, ok := m.openRewordPopup()
	rp := layerOf[*rewordPopup](m)
	if !ok || rp == nil {
		t.Fatalf("popup did not open")
	}
	if rp.commit != "abc1234def" {
		t.Fatalf("target commit = %q", rp.commit)
	}
	if rp.popup.title.Value() != "old subject" {
		t.Fatalf("title not prefilled from subject: %q", rp.popup.title.Value())
	}
}

func TestRewordPopupPrefillsFullBody(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log -1 --pretty=%B", gitexec.Result{Stdout: "real subject\n\nthe body line\n"})
	m := footerModel()
	m.svc = domain.New(&git.Repo{Runner: f})
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "abc1234def", Subject: "stale subject"}}

	m, ok := m.openRewordPopup()
	if !ok {
		t.Fatalf("popup did not open")
	}
	rp := layerOf[*rewordPopup](m)
	if rp.popup.title.Value() != "real subject" {
		t.Fatalf("title = %q, want the full message's subject", rp.popup.title.Value())
	}
	if rp.popup.desc.Value() != "the body line" {
		t.Fatalf("body = %q, want the full message's body (no silent drop)", rp.popup.desc.Value())
	}
}

// TestRewordPopupCtrlGIsNoOp guards the load-bearing invariant that ctrl+g
// never triggers the commit-generate mechanic through the shared
// applyEditKey path: rewordPopup.update calls p.popup.applyEditKey directly
// (unlike commitPopup.update, which intercepts ctrl+g before applyEditKey),
// so reword (and irebase's reword sub-mode, which shares applyEditKey the
// same way) has no dispatch hook at all — there is no staged index to
// generate a message from. ctrl+g must fall through as a harmless,
// unhandled edit key: no generating state, no dispatched cmd, no field
// mutation, no layer change.
func TestRewordPopupCtrlGIsNoOp(t *testing.T) {
	m := rewordModel(t)
	m, ok := m.openRewordPopup()
	if !ok {
		t.Fatalf("popup did not open")
	}
	before := layerOf[*rewordPopup](m)
	if before == nil {
		t.Fatalf("reword popup not on layer stack")
	}
	title, desc := before.popup.title.Value(), before.popup.desc.Value()

	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = tm.(Model)
	if cmd != nil {
		t.Fatal("ctrl+g in the reword popup must not dispatch a cmd")
	}
	rp := layerOf[*rewordPopup](m)
	if rp == nil {
		t.Fatalf("ctrl+g must not close the reword popup")
	}
	if rp.popup.generating {
		t.Fatal("ctrl+g in the reword popup must not start generating")
	}
	if rp.popup.title.Value() != title || rp.popup.desc.Value() != desc {
		t.Fatalf("ctrl+g must not mutate fields: title=%q desc=%q", rp.popup.title.Value(), rp.popup.desc.Value())
	}
}

func TestRewordMenuRowPresentOnCommits(t *testing.T) {
	m := rewordModel(t)
	if got := ids(availableActions(m)); !got["reword-commit"] {
		t.Fatalf("Commits panel menu should offer reword-commit, got %v", got)
	}
	m.focus = panelBranches
	if got := ids(availableActions(m)); got["reword-commit"] {
		t.Fatalf("reword-commit must not appear off the Commits panel, got %v", got)
	}
}
