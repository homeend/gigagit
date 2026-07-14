package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

func TestBookmarkPopupMaximizeWidensAndLiftsRowCap(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &bookmarkPopup{}
	for i := 0; i < 30; i++ { // more than the fixed cap of 12
		p.items = append(p.items, model.Bookmark{ID: fmt.Sprintf("b%d", i), Path: fmt.Sprintf("path/to/file%d", i)})
		p.rows = append(p.rows, fmt.Sprintf("path/to/file%d", i))
	}

	normal := m.renderBookmarkPopupBox(p)
	p.maximized = true
	maxed := m.renderBookmarkPopupBox(p)

	if lipgloss.Width(maxed) <= lipgloss.Width(normal) {
		t.Fatalf("maximized width %d must exceed normal %d", lipgloss.Width(maxed), lipgloss.Width(normal))
	}
	if lipgloss.Height(maxed) <= lipgloss.Height(normal) {
		t.Fatalf("maximized must show more rows: height %d vs %d", lipgloss.Height(maxed), lipgloss.Height(normal))
	}
}

func TestBookmarkPopupTKeyDoesNotMaximizeWhileFiltering(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &bookmarkPopup{filtering: true}
	p.update(m, runeKey("T"))
	if p.maximized {
		t.Fatal(`"T" while filtering must not maximize`)
	}
	if p.filter != "T" {
		t.Fatalf(`"T" while filtering must be a literal char; filter=%q`, p.filter)
	}
}

func bookmarkCopyModel(items ...model.Bookmark) Model {
	m := footerModel()
	m.width, m.height = 100, 30
	m = m.pushLayer(newBookmarkPopup(items))
	return m
}

func TestBookmarkPopupYOpensCopyChooser(t *testing.T) {
	m := bookmarkCopyModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "dir/y.go"})
	mm, _ := m.Update(keyMsg("y"))
	m = mm.(Model)
	if m.modal == nil || m.modal.req.ID != "copy-file" {
		t.Fatalf("y should open the copy chooser modal, modal=%+v", m.modal)
	}
	if !strings.Contains(m.modal.req.Prompt, "dir/y.go") {
		t.Errorf("prompt should name the bookmark's path, got %q", m.modal.req.Prompt)
	}
	if m.bookmarkSwitcher() == nil {
		t.Error("the switcher must stay on the stack beneath the modal")
	}
}

func TestBookmarkPopupYChooserHasAbsoluteOnOriginWorktree(t *testing.T) {
	m := bookmarkCopyModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "dir/y.go"})
	mm, _ := m.Update(keyMsg("y"))
	m = mm.(Model)
	if m.modal == nil {
		t.Fatal("y should open the copy chooser")
	}
	const want = "Copy file path|Copy absolute file path|Copy file name|Cancel"
	if got := strings.Join(m.modal.req.Options, "|"); got != want {
		t.Errorf("options = %q, want %q", got, want)
	}
	// The absolute option resolves against the bookmark's OWN worktree (/wt),
	// not the current worktree (/repo). This pins the copyFilePrompt(b.Worktree,
	// …) wiring: passing "" would capture /repo/dir/y.go and fail here.
	if got := m.modal.copyTexts["Copy absolute file path"]; got != "/wt/dir/y.go" {
		t.Errorf("captured abs = %q, want /wt/dir/y.go (bookmark's own worktree)", got)
	}
}

func TestBookmarkPopupYOnCommitBookmarkNotices(t *testing.T) {
	m := bookmarkCopyModel(model.Bookmark{ID: "cb", State: model.StateCommitted, Commit: "a1b2c3d4e5"})
	mm, _ := m.Update(keyMsg("y"))
	m = mm.(Model)
	if m.modal != nil {
		t.Fatal("y must not open the chooser for a commit bookmark")
	}
	if !strings.Contains(m.statusMsg, "commit bookmark") {
		t.Errorf("statusMsg = %q, want the commit-bookmark notice", m.statusMsg)
	}
}

func TestBookmarkPopupYInertInCompareMode(t *testing.T) {
	m := bookmarkCopyModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "y.go"})
	m.bookmarkSwitcher().compareRef = &model.FileRef{Source: model.SourceUnstaged, Path: "focused.go"}
	mm, _ := m.Update(keyMsg("y"))
	m = mm.(Model)
	if m.modal != nil {
		t.Fatal("y must be inert in compare-picker mode")
	}
}

func TestBookmarkPopupYWhileFilteringIsText(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &bookmarkPopup{filtering: true}
	p.update(m, runeKey("y"))
	if p.filter != "y" {
		t.Fatalf(`"y" while filtering must be a literal char; filter=%q`, p.filter)
	}
}

func TestBookmarkPopupAdvertisesCopy(t *testing.T) {
	m := bookmarkCopyModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "y.go"})
	if out := m.renderBookmarkPopupBox(m.bookmarkSwitcher()); !strings.Contains(out, "[y] copy") {
		t.Errorf("hint line missing [y] copy:\n%s", out)
	}
	found := false
	for _, l := range bookmarkSwitcherHelp(false) {
		if strings.HasPrefix(l.text, "y ") {
			found = true
		}
	}
	if !found {
		t.Error("bookmarkSwitcherHelp(false) missing the y row")
	}
}
