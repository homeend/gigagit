package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

func rewordModel(t *testing.T) Model {
	t.Helper()
	m := footerModel()
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "abc1234def", Subject: "old subject", Parents: []string{"p"}}}
	return m
}

func TestRewordPopupOpensPrefilledFromSubject(t *testing.T) {
	m := rewordModel(t)
	m, ok := m.openRewordPopup()
	if !ok || m.rewordPopup == nil {
		t.Fatalf("popup did not open")
	}
	if m.rewordPopup.commit != "abc1234def" {
		t.Fatalf("target commit = %q", m.rewordPopup.commit)
	}
	if m.rewordPopup.popup.title != "old subject" {
		t.Fatalf("title not prefilled from subject: %q", m.rewordPopup.popup.title)
	}
}

func TestRewordPopupSubmitGatedUntilLoaded(t *testing.T) {
	m := rewordModel(t)
	m, _ = m.openRewordPopup()
	// ctrl+s before the prefill lands must NOT submit (loaded == false).
	res, cmd := m.updateRewordPopupKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if res.(Model).rewordPopup == nil {
		t.Fatalf("submit should be refused while the full message is still loading")
	}
	if cmd != nil {
		t.Fatalf("no op should start before the message loads")
	}
}

func TestRewordPrefillMsgReleasesGate(t *testing.T) {
	m := rewordModel(t)
	m, _ = m.openRewordPopup()
	out, _ := m.Update(rewordPrefillMsg{commit: "abc1234def", msg: "old subject\n\nthe body"})
	m = out.(Model)
	if !m.rewordPopup.loaded {
		t.Fatalf("loaded gate not released after prefill")
	}
	if m.rewordPopup.popup.desc != "the body" {
		t.Fatalf("body not prefilled: %q", m.rewordPopup.popup.desc)
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
