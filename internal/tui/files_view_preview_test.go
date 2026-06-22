package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

// previewModel mirrors allFilesModel but also answers `git show` so View file can
// load content.
func previewModel() Model {
	return previewModelN("alpha\nbeta\ngamma\n")
}

// previewModelN is previewModel with caller-supplied `git show` content, so tests
// can preview a long file (more lines than the window can show).
func previewModelN(showOut string) Model {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log (commit files)", gitexec.Result{Stdout: "M\tinternal/tui/model.go\n"})
	f.SetResponse("git ls-tree (tree files)", gitexec.Result{Stdout: "README.md\x00pkg/sub/x.go\x00"})
	f.SetResponse("git show", gitexec.Result{Stdout: showOut})
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
	return fullTreeTreeSideOf(t, previewModel())
}

func fullTreeTreeSideOf(t *testing.T, base Model) Model {
	t.Helper()
	m := openFilesView(t, base)
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
	// Changed-files mode: now ALSO offered (shows the file's content at the commit).
	cf := m
	cf.filesAllFiles = false
	if !hasRow(cf, "view-file") {
		t.Error("View file should be offered in changed-files mode too")
	}
	// List side: not offered.
	ls := m
	ls.filesTreeFocused = false
	if hasRow(ls, "view-file") {
		t.Error("View file must not be offered on the commit-list side")
	}
}

// A deleted file has no content at the commit, so View file must not be offered.
func TestViewFileExcludesDeleted(t *testing.T) {
	m := fullTreeTreeSide(t)
	m.filesAllFiles = false
	m.filesView = &contentPopup{lines: []contentLine{{text: "D  gone.go", path: "gone.go", status: "D"}}}
	m.filesView.sel = 0
	if _, ok := m.viewFileRow(); ok {
		t.Error("View file must not be offered on a deleted (D) row")
	}
}

// Compare mode has two endpoints, not a single commit, so View file is skipped.
func TestViewFileExcludesCompareMode(t *testing.T) {
	m := fullTreeTreeSide(t)
	m.filesCompare = true
	if _, ok := m.viewFileRow(); ok {
		t.Error("View file must not be offered in compare mode")
	}
}

// End-to-end: View file works in the default changed-files view, not just the
// full tree — it opens the right-column preview with the file's content.
func TestViewFileChangedModeOpensPreview(t *testing.T) {
	m := openFilesView(t, previewModel()) // changed-files mode (no `a` toggle)
	if m.filesAllFiles {
		t.Fatal("expected changed-files mode")
	}
	m.filesTreeFocused = true
	for i, l := range m.filesView.visible() {
		if l.path != "" {
			m.filesView.sel = i
			break
		}
	}
	m = openPreview(t, m)
	if m.filesPreview == nil {
		t.Fatal("View file in changed mode should open the preview")
	}
	if body := strings.Join(linesText(m.filesPreview), "\n"); !strings.Contains(body, "alpha") {
		t.Fatalf("changed-mode preview did not load content:\n%s", body)
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
	var b strings.Builder
	for i := 0; i < 200; i++ { // longer than the window so the pager can scroll
		fmt.Fprintf(&b, "LINE%03d\n", i)
	}
	m := openPreview(t, fullTreeTreeSideOf(t, previewModelN(b.String())))
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
	// Closing the preview returns focus to the tree — the source of View file —
	// not the commit-list side (the preview had stolen focus on open).
	if !m.filesTreeFocused {
		t.Fatal("esc on the preview must return focus to the file tree, not the commit list")
	}
}

// TestFilePreviewScrollsViewportImmediately pins the pager behavior: because the
// preview has no visible cursor, every ↓ must scroll the viewport by one line —
// the top line moves off on the very first press (not after the cursor walks to
// the middle of the window, which is how a centered list would behave).
func TestFilePreviewScrollsViewportImmediately(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "LINE%03d\n", i)
	}
	m := openPreview(t, fullTreeTreeSideOf(t, previewModelN(b.String())))

	g := m.layout()
	out := m.renderFilePreview(g.rightW, g.boxH[panelCommits])
	if !strings.Contains(out, "LINE000") {
		t.Fatalf("preview should open showing the first line:\n%s", out)
	}

	m, _ = feedFilesView(t, m, "j") // one ↓
	out = m.renderFilePreview(g.rightW, g.boxH[panelCommits])
	if strings.Contains(out, "LINE000") {
		t.Fatalf("after one ↓ the first line must scroll off the top (pager), got:\n%s", out)
	}
	if !strings.Contains(out, "LINE001") {
		t.Fatalf("after one ↓ LINE001 should be the new top line:\n%s", out)
	}
}

// TestFilePreviewMouseWheelScrolls locks the mouse path: wheeling over the right
// column with a preview open must scroll the preview, not reload a commit under
// it (keyboard tests don't exercise the mouse dispatch).
func TestFilePreviewMouseWheelScrolls(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "LINE%03d\n", i)
	}
	m := openPreview(t, fullTreeTreeSideOf(t, previewModelN(b.String())))
	hashBefore := m.filesHash

	g := m.layout()
	x := g.leftW + 1 // inside the right column (the preview)
	u, _ := m.Update(tea.MouseMsg{X: x, Y: 4, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	mm := u.(Model)

	if mm.filesPreview == nil {
		t.Fatal("wheel must not close the preview")
	}
	if mm.filesPreview.sel == 0 {
		t.Fatal("wheel over the preview should scroll it (sel advanced past 0)")
	}
	if mm.filesHash != hashBefore {
		t.Fatalf("wheel must not reload a commit under the preview: hash %q → %q", hashBefore, mm.filesHash)
	}
}

func TestFilePreviewClearedByAToggle(t *testing.T) {
	m := openPreview(t, fullTreeTreeSide(t))
	m, _ = feedFilesView(t, m, "a") // leaving full-tree mode drops the preview
	if m.filesPreview != nil {
		t.Fatal("toggling all-files off should clear the preview")
	}
}
