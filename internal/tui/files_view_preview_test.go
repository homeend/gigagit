package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

// previewModel mirrors allFilesModel but also answers `git show` so View file can
// load content.
func previewModel() Model {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log (commit files)", gitexec.Result{Stdout: "M\tinternal/tui/model.go\n"})
	f.SetResponse("git ls-tree (tree files)", gitexec.Result{Stdout: "README.md\x00pkg/sub/x.go\x00"})
	f.SetResponse("git show", gitexec.Result{Stdout: "alpha\nbeta\ngamma\n"})
	return Model{
		svc:   domain.New(&git.Repo{Runner: f}),
		width: 100, height: 30,
		commits:   []model.Commit{{Hash: "1111111aaaa", Subject: "one"}, {Hash: "2222222bbbb", Subject: "two"}},
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		focus:     panelCommits,
	}
}

// fullTreeTreeSide opens the files view, switches to full-tree mode, focuses the
// tree, and lands the cursor on a real file row.
func fullTreeTreeSide(t *testing.T) Model {
	t.Helper()
	m := openFilesView(t, previewModel())
	m, cmd := feedFilesView(t, m, "a")
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	m.filesTreeFocused = true
	for i, l := range m.filesView.visible() {
		if l.path != "" {
			m.filesView.sel = i
			break
		}
	}
	return m
}

func hasRow(m Model, id string) bool {
	for _, r := range availableActions(m) {
		if r.id == id {
			return true
		}
	}
	return false
}

func TestViewFileRowGating(t *testing.T) {
	m := fullTreeTreeSide(t)
	if !hasRow(m, "view-file") {
		t.Fatal("View file should be offered in full-tree mode on a file (tree side)")
	}
	// Changed-files mode: not offered.
	cf := m
	cf.filesAllFiles = false
	if hasRow(cf, "view-file") {
		t.Error("View file must not be offered in changed-files mode")
	}
	// List side: not offered.
	ls := m
	ls.filesTreeFocused = false
	if hasRow(ls, "view-file") {
		t.Error("View file must not be offered on the commit-list side")
	}
}

func TestViewFileOpensPreview(t *testing.T) {
	m := fullTreeTreeSide(t)
	var row actionRow
	for _, r := range availableActions(m) {
		if r.id == "view-file" {
			row = r
		}
	}
	if row.run == nil {
		t.Fatal("view-file row missing a run handler")
	}
	updated, cmd := row.run(m)
	m = updated.(Model)
	if m.filesPreview == nil {
		t.Fatal("running View file should open the preview")
	}
	if m.filesTreeFocused {
		t.Fatal("opening the preview should focus the right (preview) side")
	}
	if cmd == nil {
		t.Fatal("View file should dispatch a content load")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	body := strings.Join(linesText(m.filesPreview), "\n")
	if !strings.Contains(body, "alpha") || !strings.Contains(body, "gamma") {
		t.Fatalf("preview did not load the file content:\n%s", body)
	}
}

func TestFilePreviewRendersInRightColumn(t *testing.T) {
	m := fullTreeTreeSide(t)
	for _, r := range availableActions(m) {
		if r.id == "view-file" {
			updated, cmd := r.run(m)
			m = updated.(Model)
			updated, _ = m.Update(cmd())
			m = updated.(Model)
		}
	}
	out := m.render()
	if !strings.Contains(out, "alpha") {
		t.Fatalf("right-pane preview should show the file content:\n%s", out)
	}
	if strings.Contains(out, "Commits (") {
		t.Fatalf("the Commits panel should be replaced by the preview:\n%s", out)
	}
	if !strings.Contains(out, "README.md") { // the tree stays on the left
		t.Fatalf("the file tree should remain visible:\n%s", out)
	}
}

func openPreview(t *testing.T, m Model) Model {
	t.Helper()
	for _, r := range availableActions(m) {
		if r.id == "view-file" {
			updated, cmd := r.run(m)
			m = updated.(Model)
			updated, _ = m.Update(cmd())
			return updated.(Model)
		}
	}
	t.Fatal("view-file row not found")
	return m
}

func TestFilePreviewScrollAndClose(t *testing.T) {
	m := openPreview(t, fullTreeTreeSide(t))
	if m.filesTreeFocused {
		t.Fatal("preview should be focused after opening")
	}
	m, _ = feedFilesView(t, m, "j") // right side focused → scroll the preview
	if m.filesPreview.sel != 1 {
		t.Fatalf("j should scroll the preview, sel=%d", m.filesPreview.sel)
	}
	m, _ = feedFilesView(t, m, "esc")
	if m.filesPreview != nil {
		t.Fatal("esc should close the preview")
	}
	if m.filesView == nil {
		t.Fatal("esc on the preview must NOT close the whole files view")
	}
}

func TestFilePreviewClearedByAToggle(t *testing.T) {
	m := openPreview(t, fullTreeTreeSide(t))
	m, _ = feedFilesView(t, m, "a") // leaving full-tree mode drops the preview
	if m.filesPreview != nil {
		t.Fatal("toggling all-files off should clear the preview")
	}
}
