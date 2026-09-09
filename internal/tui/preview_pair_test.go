package tui

import (
	"strings"
	"testing"
)

// TestPairPickerOffersMergePreviewDialog drives the whole pair-picker lane: the
// Branches m+m popup must offer the merge-preview row, and the row's dialog must
// swap, show once (transient, empty id) and show-and-save (a duplicate focuses
// the existing record instead of adding a second one).
func TestPairPickerOffersMergePreviewDialog(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m.width, m.height = 160, 40
	m = m.activateTab(panelBranches)
	// Mark feat/x, move to main, pair.
	m.sel[panelBranches] = indexOfBranch(m, "feat/x")
	m = pressRune(t, m, "m")
	m.sel[panelBranches] = indexOfBranch(m, "main")
	m = pressPair(t, m)
	p := layerOf[*pairOpPopup](m)
	if p == nil {
		t.Fatal("pair popup")
	}
	var row *pairOp
	for i := range p.ops {
		if strings.HasPrefix(p.ops[i].label("feat/x", "main"), "Merge preview feat/x → main") {
			row = &p.ops[i]
		}
	}
	if row == nil {
		t.Fatal("pair popup must offer the merge preview row")
	}
	m = m.popLayer()
	m, _ = row.open(m, "feat/x", "main")
	if m.modal == nil || m.modal.req.ID != "preview-pair" || !strings.Contains(m.modal.req.Prompt, "feat/x into main") {
		t.Fatalf("dialog = %+v", m.modal)
	}
	updated, _ := m.modal.onResolve(m, "swap direction")
	m = updated.(Model)
	if m.modal == nil || !strings.Contains(m.modal.req.Prompt, "main into feat/x") {
		t.Fatal("swap must re-render the dialog reversed")
	}
	updated, _ = m.modal.onResolve(m, "swap direction")
	m = updated.(Model)
	updated, cmd := m.modal.onResolve(m, "show and save")
	m = updated.(Model)
	m.modal = nil // resolveModal clears the modal before handing over; mirror it
	m = drainMsgs(t, m, cmd, 6)
	if m.filesView == nil || m.previewOpen == nil || m.previewOpen.id == "" {
		t.Fatal("show and save must save the pair and open it")
	}
	if len(m.previews) != 1 { // the fixture's pair IS feat/x → main: saving again focuses it, no duplicate
		t.Fatalf("previews = %+v", m.previews)
	}
	// fromTab=false: the dialog fires from Branches, so the save focuses the
	// record's row but must NOT activate the Previews tab behind the view.
	if m.activeLeftTab != panelBranches || m.focus == panelPreviews {
		t.Fatalf("the pair dialog must not yank focus onto Previews: tab=%v focus=%v", m.activeLeftTab, m.focus)
	}
	// show once: opens with an empty id and saves nothing.
	m = m.closeFilesView()
	m, _ = row.open(m, "feat/x", "main")
	updated, cmd = m.modal.onResolve(m, "show once")
	m = updated.(Model)
	m.modal = nil
	m = drainMsgs(t, m, cmd, 4)
	if m.previewOpen == nil || m.previewOpen.id != "" {
		t.Fatal("show once must open transiently")
	}
	if len(m.previews) != 1 {
		t.Fatalf("show once must save nothing, previews = %+v", m.previews)
	}
}

// TestPreviewPairDialogEscCancels pins the fourth option's VALUE: esc resolves
// through abortOption, which only finds "abort" — a "cancel" spelling would
// still work by the last-option fallback, but nothing else in the modal
// vocabulary would.
func TestPreviewPairDialogEscCancels(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m, _ = m.openPreviewPairDialog("feat/x", "main")
	if m.modal == nil {
		t.Fatal("dialog must open")
	}
	if got := abortOption(m.modal.req.Options); got != "abort" {
		t.Fatalf("esc option = %q, want abort", got)
	}
	updated, cmd := m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.modal != nil || cmd != nil {
		t.Fatalf("esc must close the dialog doing nothing: modal=%v cmd=%v", m.modal, cmd != nil)
	}
	if m.filesView != nil || m.previewOpen != nil {
		t.Fatal("esc must not open anything")
	}
}

func indexOfBranch(m Model, name string) int {
	for i := 0; i < m.panelLen(panelBranches); i++ {
		if m.rowKeyAt(panelBranches, i) == name {
			return i
		}
	}
	return -1
}
