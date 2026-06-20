package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
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
	if rp.popup.title != "old subject" {
		t.Fatalf("title not prefilled from subject: %q", rp.popup.title)
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
	if rp.popup.title != "real subject" {
		t.Fatalf("title = %q, want the full message's subject", rp.popup.title)
	}
	if rp.popup.desc != "the body line" {
		t.Fatalf("body = %q, want the full message's body (no silent drop)", rp.popup.desc)
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
