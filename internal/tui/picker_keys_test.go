package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/hunkpick"
)

// Picker key contract: enter walks the regions (next undecided in the conflict
// picker, next hunk in the staging pickers — both wrap), ctrl+s applies.

// captureApply swaps the picker's apply for one that records the assembled
// content, so apply tests need no service.
func captureApply(e *hunkPicker) *[]byte {
	var got []byte
	e.apply = func(m Model, content []byte) (Model, tea.Cmd) {
		got = content
		return m, nil
	}
	return &got
}

func stageDoc() *hunkpick.Doc {
	d := hunkpick.FromDiff([]byte("a\nb\nc\n"), []byte("A\nb\nC\n"))
	d.SetAll(hunkpick.TakeCurrent) // as the loader does: default = nothing staged
	return d
}

func TestConflictPickerEnterWalksUndecidedAndWraps(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.txt", pickerDoc()) // 2 regions
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("enter"))
	if e.bi != 1 {
		t.Fatalf("enter must move to the next undecided region, bi=%d", e.bi)
	}
	m, _ = e.update(m, keyMsg("enter"))
	if e.bi != 0 {
		t.Fatalf("enter at the last region must wrap to the first, bi=%d", e.bi)
	}
	m, _ = e.update(m, keyMsg("c")) // decide region 0
	m, _ = e.update(m, keyMsg("enter"))
	if e.bi != 1 {
		t.Fatalf("enter must skip decided regions, bi=%d", e.bi)
	}
	m, _ = e.update(m, keyMsg("c")) // decide region 1 — nothing pending
	m.statusMsg = ""
	got := captureApply(e)
	m, _ = e.update(m, keyMsg("enter"))
	if *got != nil {
		t.Fatal("enter must never apply")
	}
	if m.statusMsg == "" || !strings.Contains(m.statusMsg, "ctrl+s") {
		t.Fatalf("enter with nothing pending must point at ctrl+s, got %q", m.statusMsg)
	}
	if m.topLayer() == nil {
		t.Fatal("enter must keep the surface")
	}
}

func TestConflictPickerCtrlSGateAndApply(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	got := captureApply(e)
	e.bi = 1
	m, _ = e.update(m, key("ctrl+s"))
	if *got != nil || m.statusMsg == "" || e.bi != 0 {
		t.Fatalf("ctrl+s with pending regions must refuse, warn and jump to the first undecided (got applied=%v status=%q bi=%d)", *got != nil, m.statusMsg, e.bi)
	}
	m, _ = e.update(m, keyMsg("C")) // decide all: current everywhere
	m, _ = e.update(m, key("ctrl+s"))
	if *got == nil {
		t.Fatal("ctrl+s with every region decided must apply")
	}
	if string(*got) != "top\nfoo\nmid\nA\nB\n" {
		t.Fatalf("assembled content = %q", *got)
	}
}

func TestStagePickerEnterWalksHunksCtrlSApplies(t *testing.T) {
	t.Parallel()
	e := newStagePicker("f.txt", stageDoc()) // 2 hunks (a→A, c→C)
	if len(e.blocks) != 2 {
		t.Fatalf("fixture: want 2 hunks, got %d", len(e.blocks))
	}
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	got := captureApply(e)
	m, _ = e.update(m, keyMsg("enter"))
	if e.bi != 1 || *got != nil {
		t.Fatalf("enter must step to the next hunk without applying, bi=%d applied=%v", e.bi, *got != nil)
	}
	m, _ = e.update(m, keyMsg("enter"))
	if e.bi != 0 {
		t.Fatalf("enter must wrap from the last hunk, bi=%d", e.bi)
	}
	m, _ = e.update(m, key("ctrl+s"))
	if *got == nil {
		t.Fatal("ctrl+s must apply the staging picker (no gate)")
	}
}

func TestPickerCtrlSWorksUnderOutputFocus(t *testing.T) {
	t.Parallel()
	e := newStagePicker("f.txt", stageDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	got := captureApply(e)
	m, _ = e.update(m, keyMsg("tab"))
	if !e.outFocused {
		t.Fatal("tab must focus the output pane")
	}
	m, _ = e.update(m, key("ctrl+s"))
	if *got == nil {
		t.Fatal("ctrl+s must apply while the output pane is focused")
	}
}

func TestPickerEnterUnderOutputFocusReturnsGrid(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	m, _ = e.update(m, keyMsg("tab"))
	m, _ = e.update(m, keyMsg("down"))
	m, _ = e.update(m, keyMsg("enter"))
	if e.bi != 1 {
		t.Fatalf("enter must walk to the next undecided region under output focus, bi=%d", e.bi)
	}
	if e.outFocused || e.oshift != 0 {
		t.Fatalf("enter moved the cursor: focus must return to the grid (focused=%v oshift=%d)", e.outFocused, e.oshift)
	}
}

func TestPickerHintsNameNextPrevAndCtrlS(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 160, height: 30}
	out := e.render(m, "")
	for _, tok := range []string{"[n] next hunk", "[p] prev hunk", "[enter] next unresolved", "[ctrl+s] apply"} {
		if !strings.Contains(out, tok) {
			t.Errorf("conflict picker hint lacks %q:\n%s", tok, out)
		}
	}
	if strings.Contains(out, "[enter] apply") || strings.Contains(out, "[n/p] hunk") {
		t.Errorf("stale hint tokens survive:\n%s", out)
	}
	s := newStagePicker("f.txt", stageDoc())
	out = s.render(m, "")
	for _, tok := range []string{"[n] next hunk", "[p] prev hunk", "[enter] next hunk", "[ctrl+s] apply"} {
		if !strings.Contains(out, tok) {
			t.Errorf("stage picker hint lacks %q:\n%s", tok, out)
		}
	}
	m, _ = e.update(m, keyMsg("tab")) // output-pane strip
	out = e.render(m, "")
	if !strings.Contains(out, "[ctrl+s] apply") || strings.Contains(out, "[enter] apply") {
		t.Errorf("output-pane hint must advertise ctrl+s apply:\n%s", out)
	}
}

// The process-owned picker: ctrl+s with every region decided starts the
// resolve job (Working, picker dropped); enter only walks.
func TestConflictProcessPickerCtrlSApplies(t *testing.T) {
	t.Parallel()
	m := conflictRepoTUI(t) // a live service: ctrl+s really starts the resolve op
	m, _ = startConflictProcess(m)
	m.proc.(*conflictProcess).st = confWorking
	content := []byte("<<<<<<< ours\nours\n=======\ntheirs\n>>>>>>> theirs\n")
	u, _ := m.Update(conflictFileLoadedMsg{path: "uu.txt", content: content})
	m = u.(Model)
	cp := m.proc.(*conflictProcess)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if cp.st != confPicking || cp.picker == nil {
		t.Fatal("enter in the process picker must not apply")
	}
	u, _ = m.Update(keyMsg("C"))
	m = u.(Model)
	u, cmd := m.Update(key("ctrl+s"))
	m = u.(Model)
	cp = m.proc.(*conflictProcess)
	if cp.st != confWorking || cp.picker != nil || cmd == nil {
		t.Fatalf("ctrl+s must start the resolve job: st=%d picker=%v cmd=%v", cp.st, cp.picker != nil, cmd != nil)
	}
}
