package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// Files-panel enter on a conflicted row opens the region picker for that file
// directly — no conflict process in between — and apply/esc land back on the
// Files panel.

func TestFilesEnterOnBothSidesConflictLoadsPicker(t *testing.T) {
	t.Parallel()
	m := conflictModel()
	m.sel[panelFiles] = 0 // uu.txt — both sides
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if cmd == nil {
		t.Fatal("enter on a both-sides conflict must start the conflict-file load")
	}
	if m.proc != nil {
		t.Fatal("the Files-panel path must not start the conflict process")
	}
	if m.topLayer() != nil {
		t.Fatal("no surface until the file arrives")
	}
	content := []byte("<<<<<<< ours\nours\n=======\ntheirs\n>>>>>>> theirs\n")
	u, _ = m.Update(conflictFileLoadedMsg{path: "uu.txt", content: content})
	m = u.(Model)
	p, ok := m.topLayer().(*hunkPicker)
	if !ok {
		t.Fatal("the loaded file must push the standalone picker on the surface stack")
	}
	if p.title == "" || p.leftLabel != "current" {
		t.Fatalf("picker must be the conflict variant, got title=%q left=%q", p.title, p.leftLabel)
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if m.topLayer() != nil || m.focus != panelFiles {
		t.Fatal("esc must pop the picker and leave focus on the Files panel")
	}
}

func TestFilesEnterOnOneSidedConflictShowsReason(t *testing.T) {
	t.Parallel()
	m := conflictModel()
	m.sel[panelFiles] = 1 // md.txt — deleted by us
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if cmd != nil {
		t.Fatal("a one-sided conflict has no regions to pick — no load")
	}
	if m.topLayer() != nil {
		t.Fatal("no surface for a one-sided conflict")
	}
	if m.statusMsg != conflictPickable(m.status.Files[1]) || m.statusMsg == "" {
		t.Fatalf("status must carry the pickable reason, got %q", m.statusMsg)
	}
}

func TestCanResolveConflictFile(t *testing.T) {
	t.Parallel()
	m := conflictModel()
	m.sel[panelFiles] = 0
	if !m.canResolveConflictFile() {
		t.Fatal("both-sides row on the Files panel must be resolvable")
	}
	if m.canShowFileDiff() {
		t.Fatal("a conflicted row keeps the diff hint off (enter resolves instead)")
	}
	m.sel[panelFiles] = 1
	if m.canResolveConflictFile() {
		t.Fatal("a one-sided row must not advertise resolve")
	}
	m.sel[panelFiles] = 0
	m.focus = panelStaged
	if m.canResolveConflictFile() {
		t.Fatal("only the Files panel resolves")
	}
	m.focus = panelFiles
	m.status.Files[0] = model.FileStatus{Path: "a.txt", Kind: model.KindTracked, Unstaged: 'M'}
	if m.canResolveConflictFile() {
		t.Fatal("a non-conflicted row must not advertise resolve")
	}
}

func TestConflictPickable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		f    model.FileStatus
		ok   bool
	}{
		{"both sides", model.FileStatus{Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'}, true},
		{"added by both", model.FileStatus{Kind: model.KindUnmerged, Staged: 'A', Unstaged: 'A'}, true},
		{"deleted by us", model.FileStatus{Kind: model.KindUnmerged, Staged: 'D', Unstaged: 'U'}, false},
		{"deleted by them", model.FileStatus{Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'D'}, false},
		{"not a conflict", model.FileStatus{Kind: model.KindTracked, Unstaged: 'M'}, false},
	}
	for _, c := range cases {
		reason := conflictPickable(c.f)
		if (reason == "") != c.ok {
			t.Errorf("%s: pickable=%v (reason %q), want %v", c.name, reason == "", reason, c.ok)
		}
	}
}

func TestParseConflictDoc(t *testing.T) {
	t.Parallel()
	good := []byte("<<<<<<< ours\nours\n=======\ntheirs\n>>>>>>> theirs\n")
	if doc, reason := parseConflictDoc(good, 0); doc == nil || reason != "" || len(doc.Blocks()) != 1 {
		t.Fatalf("well-formed conflict must parse to one block, got doc=%v reason=%q", doc != nil, reason)
	}
	if doc, reason := parseConflictDoc([]byte("\x00\x01\x02"), 0); doc != nil || reason == "" {
		t.Fatal("binary content must fail with a reason")
	}
	if doc, reason := parseConflictDoc([]byte("plain text\nno markers\n"), 0); doc != nil || reason == "" {
		t.Fatal("content without conflict regions must fail with a reason")
	}
	if doc, reason := parseConflictDoc([]byte("<<<<<<< ours\nours\n=======\ntheirs\n"), 0); doc != nil || reason == "" {
		t.Fatal("an unterminated region must fail with a reason")
	}
}

func TestFooterShowsResolveOnConflictRow(t *testing.T) {
	t.Parallel()
	m := conflictModel()
	m.sel[panelFiles] = 0
	var found bool
	for _, b := range contextBindings() {
		if b.id == "resolve-file" {
			found = true
			if !b.when(m) {
				t.Fatal("resolve-file binding must be active on a both-sides conflict row")
			}
		}
	}
	if !found {
		t.Fatal("footer registry lacks the resolve-file binding")
	}
}
