package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// typeString feeds s to the model one rune at a time. drainMsgs (the command
// drainer these tests share) lives in preview_open_test.go.
func typeString(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	return m
}

func TestAddFormCompletesNamesSwapsAndSaves(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m = m.activateTab(panelPreviews)
	updated, _ := m.Update(keyMsg("a"))
	m = updated.(Model)
	p := layerOf[*previewAddPopup](m)
	if p == nil {
		t.Fatal("a must open the add form")
	}
	m = typeString(t, m, "fea")
	if got := p.suggestions(m); len(got) == 0 || got[0] != "feat/x" {
		t.Fatalf("suggestions = %v", got)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab}) // accept + move to target
	m = updated.(Model)
	if p.source.Value() != "feat/x" || !p.onTarget {
		t.Fatalf("tab must accept the suggestion and move to target: %q %v", p.source.Value(), p.onTarget)
	}
	m = typeString(t, m, "main")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS}) // swap
	m = updated.(Model)
	if p.source.Value() != "main" || p.target.Value() != "feat/x" {
		t.Fatalf("ctrl+s must swap the fields: %q %q", p.source.Value(), p.target.Value())
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if layerOf[*previewAddPopup](m) != nil || cmd == nil {
		t.Fatal("enter on target must close the form and start the add")
	}
	m = drainMsgs(t, m, cmd, 6)
	if len(m.previews) != 2 || m.previews[m.sel[panelPreviews]].rec.Source != "main" {
		t.Fatalf("the new pair must be saved and focused: %+v sel=%d", m.previews, m.sel[panelPreviews])
	}
	if m.focus != panelPreviews || m.activeLeftTab != panelPreviews {
		t.Fatalf("the tab's own add must leave focus on Previews: focus=%v tab=%v", m.focus, m.activeLeftTab)
	}
}

func TestAddFormRefusesUnknownName(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m = m.activateTab(panelPreviews)
	updated, _ := m.Update(keyMsg("a"))
	m = typeString(t, updated.(Model), "zzz")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = typeString(t, updated.(Model), "main")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainMsgs(t, updated.(Model), cmd, 3)
	if !strings.Contains(m.statusMsg, "zzz") || len(m.previews) != 1 {
		t.Fatalf("status = %q previews = %d", m.statusMsg, len(m.previews))
	}
}

// TestAddFormDuplicateFocusesExistingRow: adding the pair that is already
// saved is not an error — the store hands back the existing record and the
// tab simply moves the cursor onto it.
func TestAddFormDuplicateFocusesExistingRow(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m = m.activateTab(panelPreviews)
	updated, _ := m.Update(keyMsg("a"))
	m = typeString(t, updated.(Model), "feat/x")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = typeString(t, updated.(Model), "main")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainMsgs(t, updated.(Model), cmd, 6)
	if len(m.previews) != 1 || m.sel[panelPreviews] != 0 {
		t.Fatalf("a duplicate must focus the existing row: %+v sel=%d", m.previews, m.sel[panelPreviews])
	}
	if strings.Contains(m.statusMsg, "error") {
		t.Fatalf("a duplicate is not an error; status = %q", m.statusMsg)
	}
}

func TestRenameDeleteSwap(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m = m.activateTab(panelPreviews)
	updated, _ := m.Update(keyMsg("e"))
	m = updated.(Model)
	if layerOf[*previewRenamePopup](m) == nil {
		t.Fatal("e must open rename")
	}
	m = typeString(t, m, " fix")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainMsgs(t, updated.(Model), cmd, 4)
	if m.previews[0].rec.Label != "login fix" {
		t.Fatalf("label = %q", m.previews[0].rec.Label)
	}
	updated, cmd = m.Update(keyMsg("s"))
	m = drainMsgs(t, updated.(Model), cmd, 4)
	if len(m.previews) != 2 || m.previews[1].rec.Source != "main" {
		t.Fatalf("s must save the reversed pair: %+v", m.previews)
	}
	updated, _ = m.Update(keyMsg("d"))
	m = updated.(Model)
	if m.modal == nil || m.modal.req.ID != "preview-remove" {
		t.Fatal("d must open the remove confirm")
	}
	updated, cmd = m.modal.onResolve(m, "Remove")
	m = drainMsgs(t, updated.(Model), cmd, 4)
	if len(m.previews) != 1 {
		t.Fatalf("after remove: %+v", m.previews)
	}
}

func TestPreviewsFooterAndMenu(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m = m.activateTab(panelPreviews)
	m.width, m.height = 120, 40
	out := ansi.Strip(m.View())
	for _, want := range []string{"[a]dd", "[e] rename", "[d]elete", "[s]wap", "[enter] open"} {
		if !strings.Contains(out, want) {
			t.Errorf("footer lacks %q:\n%s", want, out)
		}
	}
	updated, _ := m.Update(keyMsg("."))
	menu := ansi.Strip(updated.(Model).View())
	for _, want := range []string{"Open merge preview", "New merge preview…", "Rename preview…", "Remove preview…", "Save reversed preview"} {
		if !strings.Contains(menu, want) {
			t.Errorf(". menu lacks %q:\n%s", want, menu)
		}
	}
}

// TestPreviewPopupsSwallowKeys: both forms own the keyboard while open — a
// global key types into the field instead of running its action, and esc
// pops back to the tab beneath.
func TestPreviewPopupsSwallowKeys(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m = m.activateTab(panelPreviews)
	updated, _ := m.Update(keyMsg("a"))
	m = updated.(Model)
	updated, cmd := m.Update(keyMsg("p")) // p pulls when the panels own the keyboard
	m = updated.(Model)
	if cmd != nil || m.running {
		t.Fatal("p must not start a pull while the add form is open")
	}
	p := layerOf[*previewAddPopup](m)
	if p == nil || p.source.Value() != "p" {
		t.Fatalf("p must type into the focused field: %v", p)
	}
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	if layerOf[*previewAddPopup](m) != nil {
		t.Fatal("esc must close the add form")
	}

	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	updated, cmd = m.Update(keyMsg("q")) // q quits when the panels own the keyboard
	m = updated.(Model)
	rp := layerOf[*previewRenamePopup](m)
	if cmd != nil || rp == nil || rp.label.Value() != "loginq" {
		t.Fatalf("q must type into the rename field, not quit: %v", rp)
	}
	updated, _ = m.Update(keyMsg("esc"))
	if layerOf[*previewRenamePopup](updated.(Model)) != nil {
		t.Fatal("esc must close the rename form")
	}
}
