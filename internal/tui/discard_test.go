package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func fileStatus(path string, kind model.FileKind) model.FileStatus {
	return model.FileStatus{Path: path, Kind: kind, Unstaged: 'M'}
}

// discardModel builds an idle, sized model focused on the Files panel with the
// given files, ready to drive d/D through Update.
func discardModel(files []model.FileStatus) Model {
	m := New(nil)
	m.width, m.height = 80, 30
	m.loading = false
	m.focus = panelFiles
	m.status = model.WorkingTreeStatus{Files: files}
	return m
}

// canDiscard: true only on Files panel with at least one discardable row.
func TestCanDiscardGating(t *testing.T) {
	m := discardModel([]model.FileStatus{fileStatus("a.go", model.KindTracked)})
	if !m.canDiscard() {
		t.Fatal("want canDiscard on Files panel with a tracked file")
	}
	m.focus = panelStaged
	if m.canDiscard() {
		t.Fatal("canDiscard must be false on the Staged panel")
	}
	m.focus = panelFiles
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{fileStatus("c.go", model.KindUnmerged)}}
	if m.canDiscard() {
		t.Fatal("canDiscard must be false when the only row is conflicted")
	}
}

// discardTargets: marked set wins; untracked → remove, tracked → restore;
// unmerged dropped.
func TestDiscardTargetsMarked(t *testing.T) {
	m := discardModel([]model.FileStatus{
		fileStatus("edit.go", model.KindTracked),
		fileStatus("new.txt", model.KindUntracked),
		fileStatus("conflict.go", model.KindUnmerged),
	})
	m.fileMarks = map[string]bool{"edit.go": true, "new.txt": true, "conflict.go": true}
	restore, remove, n := m.discardTargets()
	if n != 2 {
		t.Fatalf("n = %d, want 2 (conflict dropped)", n)
	}
	if len(restore) != 1 || restore[0] != "edit.go" {
		t.Fatalf("restore = %v", restore)
	}
	if len(remove) != 1 || remove[0] != "new.txt" {
		t.Fatalf("remove = %v", remove)
	}
}

// discardTargets: no marks → cursor row only.
func TestDiscardTargetsCursor(t *testing.T) {
	m := discardModel([]model.FileStatus{
		fileStatus("edit.go", model.KindTracked),
		fileStatus("new.txt", model.KindUntracked),
	})
	m.sel[panelFiles] = 1 // cursor on new.txt
	restore, remove, n := m.discardTargets()
	if n != 1 || len(remove) != 1 || remove[0] != "new.txt" || len(restore) != 0 {
		t.Fatalf("restore=%v remove=%v n=%d", restore, remove, n)
	}
}

// d on an all-conflicted Files panel: canDiscard false → no modal.
func TestDiscardKeyEmptyTargetNoOp(t *testing.T) {
	m := discardModel([]model.FileStatus{fileStatus("c.go", model.KindUnmerged)})
	nm, _ := m.Update(keyMsg("d"))
	out := nm.(Model)
	if out.modal != nil {
		t.Fatal("no modal expected for an all-conflicted Files panel")
	}
}

// D refuses while a conflict exists.
func TestDiscardAllRefusesOnConflict(t *testing.T) {
	m := discardModel([]model.FileStatus{
		fileStatus("edit.go", model.KindTracked),
		fileStatus("c.go", model.KindUnmerged),
	})
	nm, _ := m.Update(keyMsg("D"))
	out := nm.(Model)
	if out.modal != nil {
		t.Fatal("D must not open a modal while conflicts exist")
	}
	if !strings.Contains(out.statusMsg, "conflict") {
		t.Fatalf("statusMsg = %q, want a conflict refusal", out.statusMsg)
	}
}

// d opens the confirm modal with the Discard/Cancel options and the filename.
func TestDiscardKeyOpensModal(t *testing.T) {
	m := discardModel([]model.FileStatus{fileStatus("edit.go", model.KindTracked)})
	nm, _ := m.Update(keyMsg("d"))
	out := nm.(Model)
	if out.modal == nil {
		t.Fatal("d should open the confirm modal")
	}
	if out.modal.req.ID != "discard" {
		t.Fatalf("modal ID = %q, want discard", out.modal.req.ID)
	}
	if got := out.modal.req.Options; len(got) != 2 || got[0] != "Discard" || got[1] != "Cancel" {
		t.Fatalf("options = %v", got)
	}
	if !strings.Contains(out.modal.req.Prompt, "edit.go") {
		t.Fatalf("prompt = %q, want the filename", out.modal.req.Prompt)
	}
}

// canDiscardAll: refuses on conflicts and on a clean (no unstaged/untracked)
// panel; true only with real changes and no conflict.
func TestCanDiscardAllGating(t *testing.T) {
	// Mixed edit + conflict: D would refuse, so canDiscardAll must be false.
	m := discardModel([]model.FileStatus{
		fileStatus("edit.go", model.KindTracked),
		fileStatus("c.go", model.KindUnmerged),
	})
	if m.canDiscardAll() {
		t.Fatal("canDiscardAll must be false while a conflict exists")
	}
	// Clean edit-only panel: true.
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{fileStatus("edit.go", model.KindTracked)}}
	if !m.canDiscardAll() {
		t.Fatal("canDiscardAll must be true for an edit-only panel")
	}
	// Staged-only (no unstaged byte, no untracked): nothing to discard.
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "staged.go", Kind: model.KindTracked, Staged: 'M', Unstaged: '.'},
	}}
	if m.canDiscardAll() {
		t.Fatal("canDiscardAll must be false when there is nothing unstaged to discard")
	}
}

// The footer must NOT advertise [D] when D would refuse (mixed conflict panel),
// keeping the footer predicate in lockstep with the dispatch gate.
func TestFooterHidesDiscardAllOnConflict(t *testing.T) {
	m := discardModel([]model.FileStatus{
		fileStatus("edit.go", model.KindTracked),
		fileStatus("c.go", model.KindUnmerged),
	})
	if line := m.footerLine(); strings.Contains(line, "[D] discard all") {
		t.Fatalf("footer must not show [D] while a conflict exists: %q", line)
	}
}

// D opens the confirm modal for an all-discard.
func TestDiscardAllOpensModal(t *testing.T) {
	m := discardModel([]model.FileStatus{fileStatus("edit.go", model.KindTracked)})
	nm, _ := m.Update(keyMsg("D"))
	out := nm.(Model)
	if out.modal == nil || out.modal.req.ID != "discard-all" {
		t.Fatalf("D should open the discard-all modal, got %+v", out.modal)
	}
	if !strings.Contains(out.modal.req.Prompt, "ALL") {
		t.Fatalf("prompt = %q, want it to mention ALL", out.modal.req.Prompt)
	}
}
