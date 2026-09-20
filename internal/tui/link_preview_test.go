package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func TestPreviewLinkForBuildsTheThreeDotForm(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.linkRepoName = "gigagit"
	got, ok := m.previewLinkFor("feat/x", "main", "", 0)
	if !ok || got != "gg://gigagit@main...feat/x" {
		t.Fatalf("previewLinkFor(repo) = %q,%v", got, ok)
	}
	got, ok = m.previewLinkFor("feat/x", "main", "a.txt", 12)
	if !ok || got != "gg://gigagit/a.txt@main...feat/x:12" {
		t.Fatalf("previewLinkFor(file:line) = %q,%v", got, ok)
	}
	if _, err := model.ParseLink(got); err != nil {
		t.Fatalf("ParseLink(%q) = %v", got, err)
	}
	// A name the grammar cannot hold is refused, never emitted.
	if bad, ok := m.previewLinkFor("feat@x", "main", "", 0); ok {
		t.Errorf("previewLinkFor with '@' in a branch = %q, want a refusal", bad)
	}
	// A path the grammar cannot hold is refused too.
	if bad, ok := m.previewLinkFor("feat/x", "main", "we@ird.go", 0); ok {
		t.Errorf("previewLinkFor with '@' in the path = %q, want a refusal", bad)
	}
}

// previewLinkFor through the absolute-local repo form: no remote name, so
// linkRepoFor falls back to m.currentWorktree.
func TestPreviewLinkForThroughTheAbsoluteLocalRepoForm(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.currentWorktree = "/tmp/x"
	got, ok := m.previewLinkFor("feat/x", "main", "a.go", 3)
	if !ok || got != "gg:///tmp/x/a.go@main...feat/x:3" {
		t.Fatalf("previewLinkFor(local repo) = %q,%v", got, ok)
	}
	if _, err := model.ParseLink(got); err != nil {
		t.Fatalf("ParseLink(%q) = %v", got, err)
	}
	// A checkout path the grammar cannot hold (an '@' — the target
	// separator) is refused, never emitted as something that reparses as a
	// different place.
	m.currentWorktree = "/home/u@corp"
	if bad, ok := m.previewLinkFor("feat/x", "main", "a.go", 3); ok {
		t.Errorf("previewLinkFor with an inexpressible checkout = %q, want a refusal", bad)
	}
}

// The Previews panel row's . menu copies the PREVIEW link.
func TestPreviewsPanelRowCopiesThePreviewLink(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m.linkRepoName = "gigagit"
	m = m.activateTab(panelPreviews)
	// A SAVED row's link says so: ?preview=<id> is the landing that reveals
	// this row again (the id is a lookup key, never part of the address).
	want := "gg://gigagit@main...feat/x?preview=" + m.previews[0].id()
	got, ok := m.contextLinkText()
	if !ok {
		t.Fatal("the Previews panel must offer a link")
	}
	if got != want {
		t.Errorf("contextLinkText = %q, want %q", got, want)
	}
	row, ok := m.contextLinkRow()
	if !ok {
		t.Fatal("no copy-link row on the Previews panel")
	}
	if row.copyText != want {
		t.Errorf("the . menu would copy %q, want %q", row.copyText, want)
	}
}

// Inside an open preview: the file tree gives the file form, the diff view the
// file:line form, and the session snapshot's cursor.link is the same string.
func TestPreviewDiffCopiesTheFileLineForm(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m.linkRepoName = "gigagit"
	m = openMergePreview(t, m)
	if m.filesPreviewSet == nil {
		t.Fatal("the preview's note set must be armed on the files view")
	}
	// The file tree (no diff open yet): the FILE form, no line.
	got, ok := m.contextLinkText()
	if !ok || got != "gg://gigagit/a.txt@main...feat/x" {
		t.Fatalf("file-tree link = %q,%v", got, ok)
	}
	// Open the file's diff and land the cursor, then copy.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	m = drainMsgs(t, m, cmd, 4)
	v := m.diffLayer()
	if v == nil || v.previewSet == nil {
		t.Fatal("the preview diff must carry previewSet")
	}
	got, ok = m.contextLinkText()
	if !ok {
		t.Fatal("the preview diff must offer a link")
	}
	if !strings.HasPrefix(got, "gg://gigagit/a.txt@main...feat/x") {
		t.Fatalf("diff link = %q, want the preview form", got)
	}
	if _, err := model.ParseLink(got); err != nil {
		t.Fatalf("ParseLink(%q) = %v", got, err)
	}
	// cursor.link in the session snapshot is the SAME string: both read
	// contextLinkText.
	snap := buildSessionSnapshot(m)
	if snap.Cursor.Link != got {
		t.Errorf("cursor.link = %q, want %q", snap.Cursor.Link, got)
	}
}

// A preview whose branch names the grammar cannot hold produces no link at
// all, rather than one that reparses as a different place.
func TestPreviewLinkRefusedForAnUnexpressibleBranch(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.linkRepoName = "gigagit"
	set := domain.PreviewNoteSet{Source: "feat:x", Target: "main", Tip: strings.Repeat("a", 40)}
	m.filesPreviewSet = &set
	if got, ok := m.previewLinkFor(set.Source, set.Target, "a.txt", 0); ok {
		t.Errorf("previewLinkFor = %q, want a refusal", got)
	}
}

// T6a (controller correction, blocking): a row inside an open preview's file
// tree that focusedBookmark declines (an unfocused tree, a directory heading
// row, a deleted file) still belongs to the open preview — it must yield the
// PAIR's own link, never fall through to a refusal or to a lower-precedence
// surface. Uses a NESTED path so the tree carries a real heading row.
func TestPreviewFileTreeFallsBackToThePairLinkNotARefusal(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.linkRepoName = "gigagit"
	set := domain.PreviewNoteSet{Source: "feat/x", Target: "main", Tip: strings.Repeat("a", 40)}
	m.filesPreviewSet = &set
	want := "gg://gigagit@main...feat/x"

	t.Run("heading row", func(t *testing.T) {
		m := m
		m.filesView = &contentPopup{lines: []contentLine{
			{text: "internal/", heading: true},
			{text: "internal/a.go", path: "internal/a.go"},
		}}
		m.filesTreeFocused = true
		m.filesView.sel = 0 // the heading row itself
		got, ok := m.contextLinkText()
		if !ok {
			t.Fatal("contextLinkText refused on a heading row inside an open preview, want the pair link")
		}
		if got != want {
			t.Errorf("contextLinkText = %q, want %q", got, want)
		}
	})

	t.Run("tree unfocused", func(t *testing.T) {
		m := m
		m.filesView = &contentPopup{lines: []contentLine{
			{text: "internal/", heading: true},
			{text: "internal/a.go", path: "internal/a.go"},
		}}
		m.filesTreeFocused = false
		m.filesView.sel = 1 // a real file row, but the tree does not own focus
		got, ok := m.contextLinkText()
		if !ok {
			t.Fatal("contextLinkText refused with the tree unfocused inside an open preview, want the pair link")
		}
		if got != want {
			t.Errorf("contextLinkText = %q, want %q", got, want)
		}
	})
}
