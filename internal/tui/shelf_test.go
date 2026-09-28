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
	if !ok || r.label != "Add 2 marked files to shelf…" {
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

// End to end over a real repo: the marked-set row opens the naming popup;
// enter there shelves every marked file as ONE files entry (the tar's
// members), never the unmarked cursor row, and the status says so. The popup
// swallows keys and esc cancels without shelving.
func TestShelfAddMarkedSetBecomesOneEntry(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hi\n")
	for _, f := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("package x // "+f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := New(domain.New(testRepo(t, dir)))
	m.svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
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
	tm, _ := r.run(m)
	m = tm.(Model)
	p, ok := m.topLayer().(*shelfSetNamePopup)
	if !ok {
		t.Fatalf("run must open the naming popup, top layer = %T", m.topLayer())
	}
	if p.name.Value() != "WIP on main" || len(p.addrs) != 2 {
		t.Fatalf("popup = name %q addrs %d, want the WIP prefill and 2 addresses", p.name.Value(), len(p.addrs))
	}
	// A global key is swallowed (no op starts, popup stays).
	tm, _ = m.Update(keyMsg("p"))
	m = tm.(Model)
	if _, still := m.topLayer().(*shelfSetNamePopup); !still || m.running {
		t.Fatalf("a global key must be swallowed by the popup (running=%v top=%T)", m.running, m.topLayer())
	}
	// esc cancels: nothing shelved.
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)
	if m.topLayer() != nil {
		t.Fatalf("esc must close the popup, top = %T", m.topLayer())
	}
	if es, _ := m.svc.ShelfList(context.Background(), "", 0, 0); len(es) != 0 {
		t.Fatalf("esc must shelve nothing, shelf has %d", len(es))
	}

	// Reopen, name it, enter.
	tm, _ = r.run(m)
	m = tm.(Model)
	p = m.topLayer().(*shelfSetNamePopup)
	p.name = newTextField("half-done feature")
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	if cmd == nil || m.topLayer() != nil {
		t.Fatalf("enter must close the popup and return the shelving command (cmd nil=%v top=%T)", cmd == nil, m.topLayer())
	}
	tm, _ = m.Update(cmd())
	m = tm.(Model)
	es, err := m.svc.ShelfList(context.Background(), "", 0, 0)
	if err != nil || len(es) != 1 {
		t.Fatalf("shelf holds %d entries (err=%v), want exactly ONE set", len(es), err)
	}
	e := es[0]
	if want := i18n.T("shelved %d files as one set → %s", 2, e.ID); m.statusMsg != want {
		t.Fatalf("statusMsg = %q, want %q", m.statusMsg, want)
	}
	if e.Kind != model.ShelfKindFiles || e.Label != "half-done feature" || e.Origin.State != model.StateUntracked || e.Origin.Worktree != dir || e.Origin.Path != "" {
		t.Fatalf("entry = %+v, want a labelled files set with a path-less untracked origin", e)
	}
	files, err := m.svc.ShelfCommitFiles(context.Background(), e.ID)
	if err != nil || len(files) != 2 || files[0].Path != "a.go" || files[1].Path != "b.go" {
		t.Fatalf("members = %+v err=%v, want a.go + b.go (README.md was not marked)", files, err)
	}
	if len(m.fileMarks) != 2 {
		t.Fatalf("marks must stay after shelving (a snapshot moves nothing), got %v", m.fileMarks)
	}
	// The shelf switcher treats the set like a shelved commit: file-only keys
	// are refused with the archive notice, and enter browses its members.
	m.shelfEntries = es
	sp := &shelfPopup{items: es, rows: []string{shelfEntryDisplay(e)}}
	m = m.pushLayer(sp)
	nm, blocked := m.commitShelfNotice(sp)
	if !blocked || !strings.Contains(nm.statusMsg, "file set") {
		t.Fatalf("file-only keys must be refused on a file set (blocked=%v msg=%q)", blocked, nm.statusMsg)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	if m.filesView == nil || !m.inShelfFiles() || m.filesShelfID != e.ID {
		t.Fatalf("enter must open the files view in shelf mode on the set (view nil=%v mode shelf=%v id=%q)", m.filesView == nil, m.inShelfFiles(), m.filesShelfID)
	}
}
