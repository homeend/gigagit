package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// conflictModel() (in conflict_popup_test.go) builds a Model whose status holds
// two unmerged files (uu.txt, md.txt).

func TestConflictProcessStartsAndLeaves(t *testing.T) {
	m := conflictModel()
	m, _ = startConflictProcess(m)

	cp, ok := m.proc.(*conflictProcess)
	if !ok {
		t.Fatalf("start must fill the slot with a conflict process, got %T", m.proc)
	}
	if cp.st != confListing {
		t.Fatalf("must start in Listing, got state %d", cp.st)
	}
	if len(cp.files) != 2 {
		t.Fatalf("must carry the 2 conflicted files, got %d", len(cp.files))
	}

	// Leave releases the slot (start-by-detection re-offers it later).
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	m = u.(Model)
	if m.proc != nil {
		t.Fatal("Leave must release the slot")
	}
}

func TestConflictProcessEscLeaves(t *testing.T) {
	m := conflictModel()
	m, _ = startConflictProcess(m)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if m.proc != nil {
		t.Fatal("esc must release the slot")
	}
}

func TestConflictProcessNoStartWithoutConflicts(t *testing.T) {
	m := Model{sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	m, _ = startConflictProcess(m)
	if m.proc != nil {
		t.Fatal("no conflicts → no process")
	}
}

func TestConflictActionForGating(t *testing.T) {
	uu := model.FileStatus{Path: "uu.txt", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'} // both sides
	du := model.FileStatus{Path: "md.txt", Kind: model.KindUnmerged, Staged: 'D', Unstaged: 'U'} // one side

	if a, ok := conflictActionFor(uu, "C"); !ok || a != engine.KeepOurs {
		t.Fatalf("both-sides + C must be KeepOurs, got %v ok=%v", a, ok)
	}
	if _, ok := conflictActionFor(uu, "d"); ok {
		t.Fatal("both-sides + d (delete) must be rejected")
	}
	if _, ok := conflictActionFor(uu, "k"); ok {
		t.Fatal("both-sides + k (keep modified) must be rejected")
	}
	if a, ok := conflictActionFor(du, "d"); !ok || a != engine.DeleteFile {
		t.Fatalf("one-sided + d must be DeleteFile, got %v ok=%v", a, ok)
	}
	if _, ok := conflictActionFor(du, "C"); ok {
		t.Fatal("one-sided + C (keep ours) must be rejected")
	}
	if _, ok := conflictActionFor(uu, "x"); ok {
		t.Fatal("an unrelated key must be rejected")
	}
}

func TestConflictProcessFinishedError(t *testing.T) {
	m := conflictModel()
	m, _ = startConflictProcess(m)
	cp := m.proc.(*conflictProcess)
	cp.st = confWorking
	m, _ = cp.finished(m, engine.Result{}, errors.New("boom"))
	if cp.st != confReporting || cp.errMsg != "boom" {
		t.Fatalf("a failed job must enter Reporting with the message, got st=%d err=%q", cp.st, cp.errMsg)
	}
	// any key acks → back to Listing
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = u.(Model)
	if m.proc.(*conflictProcess).st != confListing {
		t.Fatal("acknowledging the error must return to Listing")
	}
}

func TestConflictProcessOpFinishedRoutesToProcess(t *testing.T) {
	m := conflictModel()
	m, _ = startConflictProcess(m)
	m.proc.(*conflictProcess).st = confWorking
	u, _ := m.Update(opFinishedMsg{err: errors.New("nope")})
	m = u.(Model)
	if m.proc.(*conflictProcess).st != confReporting {
		t.Fatal("opFinishedMsg must route to the active process's finished()")
	}
}

func TestConflictProcessDataLoadedRoutesToRefreshed(t *testing.T) {
	m := conflictModel()
	m, _ = startConflictProcess(m)
	m.proc.(*conflictProcess).st = confWorking
	// a fresh load with one fewer conflict must route to refreshed() → Listing
	u, _ := m.Update(dataLoadedMsg{gen: m.loadGen, status: model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "md.txt", Kind: model.KindUnmerged, Staged: 'D', Unstaged: 'U'},
	}}})
	m = u.(Model)
	cp := m.proc.(*conflictProcess)
	if cp.st != confListing || len(cp.files) != 1 {
		t.Fatalf("dataLoadedMsg must route to refreshed → Listing with 1 file, got st=%d n=%d", cp.st, len(cp.files))
	}
}

func TestConflictProcessContinueAbortGating(t *testing.T) {
	cp := &conflictProcess{}
	// nothing resolved, not in progress
	if cp.canContinue() || cp.canAbort() {
		t.Fatal("no in-progress op → neither continue nor abort")
	}
	// resolving in progress, files remain
	cp.inProgress = "merge"
	cp.files = []model.FileStatus{{Path: "x"}}
	if cp.canContinue() {
		t.Fatal("continue must wait until every file is resolved")
	}
	if !cp.canAbort() {
		t.Fatal("abort must be available whenever a merge/rebase is in progress")
	}
	// all resolved, still in progress
	cp.files = nil
	if !cp.canContinue() {
		t.Fatal("all resolved + in progress → continue available")
	}
}

func TestConflictProcessReleasesWhenDone(t *testing.T) {
	m := conflictModel()
	m, _ = startConflictProcess(m)
	cp := m.proc.(*conflictProcess)
	cp.files = nil // everything resolved

	// the in-progress probe says the merge/rebase is over → release the slot
	u, _ := m.Update(inProgressMsg{op: ""})
	m = u.(Model)
	if m.proc != nil {
		t.Fatal("no conflicts + no in-progress op must release the slot")
	}
}

func TestConflictProcessStaysWhenResolvedButInProgress(t *testing.T) {
	m := conflictModel()
	m, _ = startConflictProcess(m)
	cp := m.proc.(*conflictProcess)
	cp.files = nil // resolved, but the merge is still in progress (awaiting continue)

	u, _ := m.Update(inProgressMsg{op: "merge"})
	m = u.(Model)
	if m.proc == nil {
		t.Fatal("resolved-but-in-progress must keep the slot (offering continue)")
	}
	if m.proc.(*conflictProcess).inProgress != "merge" {
		t.Fatal("the probe result must be recorded on the process")
	}
}

func TestConflictProcessCancelReturnsToList(t *testing.T) {
	m := conflictModel()
	m, _ = startConflictProcess(m)
	cp := m.proc.(*conflictProcess)
	cp.st = confWorking
	// a cancelled job must NOT show an error; it re-reads and returns to the list
	m2, cmd := cp.finished(m, engine.Result{}, context.Canceled)
	_ = m2
	if cp.st == confReporting {
		t.Fatal("a cancelled job must not be reported as an error")
	}
	if cmd == nil {
		t.Fatal("cancel must trigger a reload to resync state")
	}
}

func TestConflictProcessEnterLoadsBothSides(t *testing.T) {
	m := conflictModel()
	m, _ = startConflictProcess(m)
	m.proc.(*conflictProcess).sel = 0 // uu.txt — both sides
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if m.proc.(*conflictProcess).st != confWorking {
		t.Fatalf("enter on a both-sides file must load (Working), got %d", m.proc.(*conflictProcess).st)
	}
	if cmd == nil {
		t.Fatal("enter must start the conflict-file load")
	}
}

func TestConflictProcessEnterRejectsOneSided(t *testing.T) {
	m := conflictModel()
	m, _ = startConflictProcess(m)
	m.proc.(*conflictProcess).sel = 1 // md.txt — one sided
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if m.proc.(*conflictProcess).st != confListing {
		t.Fatal("enter on a one-sided file must stay in Listing")
	}
}

func TestConflictProcessFileLoadedShowsPickerEscReturns(t *testing.T) {
	m := conflictModel()
	m, _ = startConflictProcess(m)
	m.proc.(*conflictProcess).st = confWorking
	content := []byte("<<<<<<< ours\nours\n=======\ntheirs\n>>>>>>> theirs\n")
	u, _ := m.Update(conflictFileLoadedMsg{path: "uu.txt", content: content})
	m = u.(Model)
	cp := m.proc.(*conflictProcess)
	if cp.st != confPicking || cp.picker == nil {
		t.Fatalf("a loaded conflict file must show the picker (Picking), got st=%d", cp.st)
	}
	if m.topLayer() != nil {
		t.Fatal("the process owns the picker; it must NOT be pushed on the surface stack")
	}
	// esc returns to Listing and drops the picker
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	cp = m.proc.(*conflictProcess)
	if cp.st != confListing || cp.picker != nil {
		t.Fatal("esc in Picking must return to Listing and drop the picker")
	}
}

func TestConflictProcessRefreshedReLists(t *testing.T) {
	m := conflictModel() // 2 conflicts
	m, _ = startConflictProcess(m)
	cp := m.proc.(*conflictProcess)
	cp.st = confWorking
	cp.sel = 1
	// the resolve removed one file: refresh from a status with a single conflict
	m.status = model.WorkingTreeStatus{Branch: "zzz", Files: []model.FileStatus{
		{Path: "md.txt", Kind: model.KindUnmerged, Staged: 'D', Unstaged: 'U'},
	}}
	m, _ = cp.refreshed(m)
	if cp.st != confListing {
		t.Fatalf("refresh must return to Listing, got %d", cp.st)
	}
	if len(cp.files) != 1 {
		t.Fatalf("refresh must re-derive the shorter list, got %d", len(cp.files))
	}
	if cp.sel != 0 {
		t.Fatalf("sel must clamp into the shorter list, got %d", cp.sel)
	}
}

func TestConflictProcessEscRestoresPickerZoom(t *testing.T) {
	p := &conflictProcess{st: confPicking, picker: newProcessConflictPicker("f.txt", pickerDoc())}
	m := Model{proc: p, sel: map[panel]int{}, sortModes: map[panel]sortMode{}, width: 80, height: 24}
	m, _ = p.update(m, keyMsg("ctrl+t"))
	if !p.picker.zoomed {
		t.Fatalf("ctrl+t did not reach the process picker")
	}
	m, _ = p.update(m, keyMsg("esc"))
	if p.picker == nil || p.st != confPicking {
		t.Fatalf("esc under zoom left the picker (st=%v)", p.st)
	}
	if p.picker.zoomed {
		t.Fatalf("esc under zoom did not restore the split")
	}
	m, _ = p.update(m, keyMsg("esc"))
	if p.picker != nil || p.st != confListing {
		t.Fatalf("second esc did not leave the picker (st=%v)", p.st)
	}
}

func TestConflictProcessRefreshKeepsPicking(t *testing.T) {
	p := &conflictProcess{st: confPicking, picker: newProcessConflictPicker("f.txt", pickerDoc()), pickPath: "f.txt"}
	m := Model{proc: p, width: 80, height: 24}
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "f.txt", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'},
	}}
	m, _ = p.refreshed(m)
	if p.st != confPicking || p.picker == nil {
		t.Fatalf("a background refresh discarded the picking session: st=%v picker=%v", p.st, p.picker)
	}
	// The file leaving the conflict set (resolved elsewhere) falls back to
	// the list — a stale editor over a resolved file would be worse.
	m.status = model.WorkingTreeStatus{}
	m, _ = p.refreshed(m)
	if p.st != confListing || p.picker != nil {
		t.Fatalf("refresh with the file resolved must return to the list: st=%v", p.st)
	}
}
