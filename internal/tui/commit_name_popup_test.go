package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

func TestCommitNamePopupEnterDispatches(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 1)
	p := &commitNamePopup{commit: model.Commit{Hash: "a1b2c3d4e5", Subject: "subj"}, forShelf: true, name: newTextField("subj")}
	_, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should dispatch a create command")
	}
}

func TestCommitNamePopupCtrlSInsertsShortSha(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 1)
	p := &commitNamePopup{commit: model.Commit{Hash: "a1b2c3d4e5", Subject: ""}, forShelf: false, name: newTextField("")}
	p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if got := p.name.Value(); got != "a1b2c3d" {
		t.Fatalf("after ctrl+s name = %q, want the 7-char short sha", got)
	}
}

func TestCommitNamePopupEscNoDispatch(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 1)
	p := &commitNamePopup{commit: model.Commit{Hash: "a1b2c3d4e5"}, forShelf: true, name: newTextField("x")}
	_, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("esc should not dispatch a create command")
	}
}

func TestCommitBookmarkLabelFallsBackToSubject(t *testing.T) {
	t.Parallel()
	if b := commitBookmark(model.Commit{Hash: "h", Subject: "the subject"}, ""); b.Label != "the subject" {
		t.Fatalf("empty label should fall back to subject, got %q", b.Label)
	}
	if b := commitBookmark(model.Commit{Hash: "h", Subject: "the subject"}, "custom"); b.Label != "custom" {
		t.Fatalf("label = %q, want custom", b.Label)
	}
}

// The four rows below must open commitNamePopup (not dispatch a create
// command directly) — the whole point of this feature is the naming step in
// between. Each asserts forShelf/commit on the pushed popup so a copy-paste
// mistake in the rewiring (e.g. commit-bookmark wired to forShelf: true)
// fails loudly.

func TestCommitShelfRowOpensNamePopup(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	r, ok := m.commitShelfRow()
	if !ok {
		t.Fatal("commit-shelf row should be offered")
	}
	mm, _ := r.run(m)
	p := layerOf[*commitNamePopup](mm.(Model))
	if p == nil {
		t.Fatal("commit-shelf row should push a commitNamePopup")
	}
	if !p.forShelf {
		t.Fatal("commit-shelf popup must be forShelf")
	}
	if p.commit.Hash != m.commits[0].Hash || p.name.Value() != m.commits[0].Subject {
		t.Fatalf("popup should carry the selected commit + prefill its subject, got %+v", p)
	}
}

func TestCommitBookmarkRowOpensNamePopup(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	r, ok := m.commitBookmarkRow()
	if !ok {
		t.Fatal("commit-bookmark row should be offered")
	}
	mm, _ := r.run(m)
	p := layerOf[*commitNamePopup](mm.(Model))
	if p == nil {
		t.Fatal("commit-bookmark row should push a commitNamePopup")
	}
	if p.forShelf {
		t.Fatal("commit-bookmark popup must not be forShelf")
	}
	if p.commit.Hash != m.commits[0].Hash || p.name.Value() != m.commits[0].Subject {
		t.Fatalf("popup should carry the selected commit + prefill its subject, got %+v", p)
	}
}

func TestReflogShelfRowOpensNamePopup(t *testing.T) {
	t.Parallel()
	m := reflogTestModel()
	m.focus = panelReflog
	r, ok := m.reflogShelfRow()
	if !ok {
		t.Fatal("reflog-shelf row should be offered")
	}
	mm, _ := r.run(m)
	p := layerOf[*commitNamePopup](mm.(Model))
	if p == nil {
		t.Fatal("reflog-shelf row should push a commitNamePopup")
	}
	if !p.forShelf {
		t.Fatal("reflog-shelf popup must be forShelf")
	}
	if p.commit.Hash != m.reflog[0].Hash || p.name.Value() != m.reflog[0].Subject {
		t.Fatalf("popup should carry the selected reflog entry + prefill its subject, got %+v", p)
	}
}

func TestReflogBookmarkRowOpensNamePopup(t *testing.T) {
	t.Parallel()
	m := reflogTestModel()
	m.focus = panelReflog
	r, ok := m.reflogBookmarkRow()
	if !ok {
		t.Fatal("reflog-bookmark row should be offered")
	}
	mm, _ := r.run(m)
	p := layerOf[*commitNamePopup](mm.(Model))
	if p == nil {
		t.Fatal("reflog-bookmark row should push a commitNamePopup")
	}
	if p.forShelf {
		t.Fatal("reflog-bookmark popup must not be forShelf")
	}
	if p.commit.Hash != m.reflog[0].Hash || p.name.Value() != m.reflog[0].Subject {
		t.Fatalf("popup should carry the selected reflog entry + prefill its subject, got %+v", p)
	}
}
