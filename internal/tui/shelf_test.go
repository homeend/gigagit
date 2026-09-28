package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/shelf"
)

func TestAddToShelfRowOnFilesPanel(t *testing.T) {
	t.Parallel()
	m := filesMenuModel() // panelFiles focused with one tracked file
	m.currentWorktree = "/wt"
	if _, ok := findRow(availableActions(m), "shelf-add"); !ok {
		t.Fatalf("Add to shelf missing from menu on Files panel")
	}
	a, ok := m.focusedShelfAddress()
	if !ok || a.State != model.StateUnstaged || a.Path != "dir/f.txt" || a.Worktree != "/wt" {
		t.Fatalf("focusedShelfAddress = %+v ok=%v, want unstaged dir/f.txt @ /wt", a, ok)
	}
}

func TestShelfAddCaptureFromBlame(t *testing.T) {
	t.Parallel()
	// Working-tree blame (ctx.rev == "") captures the current worktree's working
	// file — the worktree/branch come from the Model (derived ad-hoc), not stored
	// on the view. Mirrors the working-tree diff-view capture.
	m := footerModel().pushLayer(blameFixture()) // ctx.rev == ""
	m.currentWorktree = "/wt"
	m.status.Branch = "main"
	a, ok := m.focusedShelfAddress()
	if !ok || a.State != model.StateUnstaged || a.Worktree != "/wt" || a.Path != "a.go" {
		t.Fatalf("working-tree blame capture = %+v ok=%v, want unstaged a.go @ /wt", a, ok)
	}
	// A committed blame captures the commit.
	m2 := footerModel().pushLayer(&blameView{ctx: navContext{path: "a.go", rev: "abc1234def"}})
	c, ok := m2.focusedShelfAddress()
	if !ok || c.State != model.StateCommitted || c.Commit != "abc1234def" || c.Path != "a.go" {
		t.Fatalf("committed blame capture = %+v ok=%v", c, ok)
	}
}

func TestAddToShelfRowAbsentWhenNoFileFocused(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.focus = panelBranches
	if _, ok := m.focusedShelfAddress(); ok {
		t.Fatalf("no file focused on Branches panel; focusedShelfAddress should be false")
	}
	if _, ok := findRow(availableActions(m), "shelf-add"); ok {
		t.Fatalf("Add to shelf should not appear with no file focused")
	}
}

func TestShelfRestorePopupRequiresDest(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m = m.pushLayer(&shelfRestorePopup{entryID: "unstaged-a-go-deadbeef", origin: "a.go"})
	// Enter with an empty dest is a no-op (popup stays open).
	u, _ := m.Update(keyMsg("enter"))
	m = u.(Model)
	if shelfRestoreOf(m) == nil {
		t.Fatalf("empty dest should keep the popup open")
	}
	// Typing builds the destination.
	for _, r := range "out.txt" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = u.(Model)
	}
	if shelfRestoreOf(m).dest.Value() != "out.txt" {
		t.Fatalf("dest = %q, want out.txt", shelfRestoreOf(m).dest.Value())
	}
}

// Marked files on the Files/Staged panel: "Add to shelf" acts on the whole
// marked set restricted to the focused panel (like space/stage), not just the
// cursor row. No marks → the cursor row, as before.
func TestShelfAddTargetsMarkedFiles(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.loading = false
	m.focus = panelFiles
	m.currentWorktree = "/wt"
	m.status.Branch = "main"
	m.status.Files = []model.FileStatus{
		{Path: "a.go", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'},
		{Path: "b.go", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'},
		{Path: "c.txt", Kind: model.KindUntracked, Staged: '?', Unstaged: '?'},
		{Path: "s.go", Kind: model.KindTracked, Staged: 'M', Unstaged: '.'}, // Staged panel only
	}
	m.sel[panelFiles] = 0
	m.fileMarks = map[string]bool{"b.go": true, "c.txt": true, "s.go": true}

	got, _ := m.shelfAddTargets()
	if len(got) != 2 {
		t.Fatalf("targets = %+v, want b.go + c.txt (s.go is not a Files-panel member)", got)
	}
	if got[0].Path != "b.go" || got[0].State != model.StateUnstaged || got[0].Worktree != "/wt" || got[0].Branch != "main" {
		t.Fatalf("first target = %+v, want unstaged b.go @ /wt on main", got[0])
	}
	if got[1].Path != "c.txt" || got[1].State != model.StateUntracked {
		t.Fatalf("second target = %+v, want untracked c.txt", got[1])
	}
	r, ok := findRow(availableActions(m), "shelf-add")
	if !ok || r.label != "Add 2 marked files to shelf" {
		t.Fatalf("menu row = %+v ok=%v, want the count-aware label", r, ok)
	}

	// Exactly one marked file away from the cursor: the label must say the
	// MARKED file is what gets shelved, not the cursor row.
	m.fileMarks = map[string]bool{"b.go": true}
	got, _ = m.shelfAddTargets()
	if len(got) != 1 || got[0].Path != "b.go" {
		t.Fatalf("single-mark targets = %+v, want b.go", got)
	}
	if r, ok := findRow(availableActions(m), "shelf-add"); !ok || r.label != "Add the marked file to shelf" {
		t.Fatalf("menu row = %+v ok=%v, want the single-marked label", r, ok)
	}
	m.fileMarks = map[string]bool{"b.go": true, "c.txt": true, "s.go": true}

	// The Staged panel sees only its own members, addressed as staged.
	m.focus = panelStaged
	m.sel[panelStaged] = 0
	got, _ = m.shelfAddTargets()
	if len(got) != 1 || got[0].Path != "s.go" || got[0].State != model.StateStaged {
		t.Fatalf("staged targets = %+v, want staged s.go only", got)
	}

	// No marks → the cursor row, single label.
	m.focus = panelFiles
	m.fileMarks = nil
	got, _ = m.shelfAddTargets()
	if len(got) != 1 || got[0].Path != "a.go" {
		t.Fatalf("unmarked targets = %+v, want cursor row a.go", got)
	}
	if r, ok := findRow(availableActions(m), "shelf-add"); !ok || r.label != "Add to shelf" {
		t.Fatalf("menu row = %+v ok=%v, want plain label", r, ok)
	}
}

// A diff view over one file keeps shelving THAT file even while marks exist on
// the panel underneath — the marked set belongs to the bare panel only.
func TestShelfAddTargetsIgnoreMarksUnderDiffView(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.focus = panelFiles
	m.currentWorktree = "/wt"
	m.status.Files = []model.FileStatus{
		{Path: "a.go", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'},
		{Path: "b.go", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'},
	}
	m.fileMarks = map[string]bool{"a.go": true, "b.go": true}
	m = m.pushLayer(&diffView{title: "a.go"})
	got, _ := m.shelfAddTargets()
	if len(got) != 1 || got[0].Path != "a.go" {
		t.Fatalf("targets under diff view = %+v, want just a.go", got)
	}
}

// End to end over a real repo: the many-file command shelves every marked
// file (one entry each) and its message lands as a "shelved N files" status.
// One unreadable path fails alone — the rest still land, and the status names
// the partial count with the error.
func TestShelfAddManyCmdShelvesEveryMarkedFile(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hi\n")
	for _, f := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("package x // "+f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := New(domain.New(testRepo(t, dir)))
	st := shelf.NewFileStore(t.TempDir())
	m.svc.SetShelfStore(st)
	m.currentWorktree = dir
	m.focus = panelFiles
	m = m.withStatus(model.WorkingTreeStatus{Branch: "main", Files: []model.FileStatus{
		{Path: "a.go", Kind: model.KindUntracked, Staged: '?', Unstaged: '?'},
		{Path: "b.go", Kind: model.KindUntracked, Staged: '?', Unstaged: '?'},
		{Path: "README.md", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'},
	}})
	m.sel[panelFiles] = 2 // cursor on README.md — NOT marked, must not be shelved
	m.fileMarks = map[string]bool{"a.go": true, "b.go": true}

	r, ok := findRow(availableActions(m), "shelf-add")
	if !ok {
		t.Fatal("Add to shelf row missing")
	}
	tm, cmd := r.run(m)
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("run must return the shelving command")
	}
	tm, _ = m.Update(cmd())
	m = tm.(Model)
	if want := i18n.T("shelved %d files", 2); m.statusMsg != want {
		t.Fatalf("statusMsg = %q, want %q", m.statusMsg, want)
	}
	if len(m.fileMarks) != 2 {
		t.Fatalf("marks must stay after shelving (a snapshot moves nothing), got %v", m.fileMarks)
	}
	es, err := m.svc.ShelfList(context.Background(), "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, e := range es {
		paths[e.Origin.Path] = true
		if e.Origin.State != model.StateUntracked || e.Origin.Worktree != dir {
			t.Fatalf("entry %+v: want an untracked address in %s", e.Origin, dir)
		}
	}
	if len(es) != 2 || !paths["a.go"] || !paths["b.go"] {
		t.Fatalf("shelf holds %v, want exactly a.go and b.go", paths)
	}

	// A missing file fails alone: the other lands and the status says 1 of 2.
	m.fileMarks = map[string]bool{"a.go": true, "gone.go": true}
	m = m.withStatus(model.WorkingTreeStatus{Branch: "main", Files: []model.FileStatus{
		{Path: "a.go", Kind: model.KindUntracked, Staged: '?', Unstaged: '?'},
		{Path: "gone.go", Kind: model.KindUntracked, Staged: '?', Unstaged: '?'},
	}})
	r, _ = findRow(availableActions(m), "shelf-add")
	tm, cmd = r.run(m)
	tm, _ = tm.(Model).Update(cmd())
	m = tm.(Model)
	if !strings.HasPrefix(m.statusMsg, "shelved 1 of 2 files: ") {
		t.Fatalf("partial statusMsg = %q, want the 1-of-2 form with the error", m.statusMsg)
	}
}
