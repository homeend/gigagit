package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// symRowModel is symModel with a saved symmetric preview (feat/x vs feat/y,
// base main) and the Previews cursor on its row.
func symRowModel(t *testing.T) Model {
	t.Helper()
	m := symModel(t)
	if _, err := m.svc.SymmetricPreviewAdd(context.Background(), "feat/x", "feat/y", "main", ""); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ := m.Update(cmd())
	m = updated.(Model).activateTab(panelPreviews)
	for i, r := range m.previews {
		if r.sym != nil {
			m.sel[panelPreviews] = i
			return m
		}
	}
	t.Fatalf("no symmetric row among %+v", m.previews)
	return m
}

func TestSymmetricRowWearsBadgeAndBase(t *testing.T) {
	t.Parallel()
	m := symRowModel(t)
	r, _ := m.selectedPreview()
	sub := r.subject()
	if !strings.HasPrefix(sub, "sym ") || !strings.Contains(sub, "feat/x vs feat/y") || !strings.Contains(sub, "base: main") {
		t.Fatalf("subject = %q; want sym, A vs B and the base", sub)
	}
	if strings.Contains(sub, "↔") {
		t.Fatalf("a symmetric row draws no ↔: %q", sub)
	}
	if out := m.View(); !strings.Contains(out, "feat/x vs feat/y") {
		t.Fatalf("the row is not painted:\n%s", out)
	}
}

// A saved comparison that is NOT symmetric keeps its <left> ↔ <right> subject.
func TestPlainComparisonRowIsNotSymmetric(t *testing.T) {
	t.Parallel()
	m := symModel(t)
	left, _ := m.previewLinkFor("feat/x", "main", "", 0)
	right, _ := m.previewLinkFor("main", "feat/y", "", 0) // different bases
	if _, err := m.svc.SavedCompareAdd(context.Background(), left, right, "mixed"); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	for _, r := range m.previews {
		if r.label() == "mixed" && (r.sym != nil || !strings.Contains(r.subject(), "↔")) {
			t.Fatalf("a non-symmetric comparison was dressed as one: %q", r.subject())
		}
	}
}

func TestSymmetricRowEnterShowsLoading(t *testing.T) {
	t.Parallel()
	m := symRowModel(t)
	m, cmd := send(m, keyType(tea.KeyEnter))
	if _, ok := m.topLayer().(*compareLoadingPopup); !ok || cmd == nil {
		t.Fatalf("enter must open behind the loading popup, top = %T", m.topLayer())
	}
	m = pumpAll(t, m, cmd)
	if _, ok := m.topLayer().(*compareLoadingPopup); ok || m.filesSets == nil {
		t.Fatalf("the comparison must land and close the popup (status %q)", m.statusMsg)
	}
}

func TestSymmetricRowDotMenuOpensEachSide(t *testing.T) {
	t.Parallel()
	m := symRowModel(t)
	rows := m.previewSymmetricSideRows()
	if len(rows) != 2 {
		t.Fatalf("want 2 side rows, got %d", len(rows))
	}
	if !strings.Contains(rows[0].label, "feat/x → main") || !strings.Contains(rows[1].label, "feat/y → main") {
		t.Fatalf("labels = %q, %q", rows[0].label, rows[1].label)
	}
	found := false
	for _, r := range availableActions(m) {
		if r.id == "preview-sym-a" {
			found = true
		}
	}
	if !found {
		t.Fatal("the . menu does not offer the side rows")
	}
	_, cmd := rows[0].run(m)
	if cmd == nil {
		t.Fatal("opening a side must start the preview open")
	}
}
