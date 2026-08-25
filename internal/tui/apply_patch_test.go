package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The palette contains an "Apply patch…" entry.
func TestPaletteHasApplyPatch(t *testing.T) {
	t.Parallel()
	found := false
	for _, c := range paletteCommands() {
		if c.label == "Apply patch…" {
			found = true
		}
	}
	if !found {
		t.Fatal("palette should list Apply patch…")
	}
}

// applyPatchDirMsg opens the popup prefilled with <dir>/; enter with a typed
// path closes the popup and dispatches the op (a non-nil tea.Cmd).
func TestApplyPatchPopupDispatchesOp(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)

	m, _ = send(m, applyPatchDirMsg{dir: "/exports"})
	p, ok := m.topLayer().(*applyPatchPopup)
	if !ok {
		t.Fatalf("top layer = %T, want *applyPatchPopup", m.topLayer())
	}
	if got := p.path.Value(); !strings.HasPrefix(got, "/exports") {
		t.Fatalf("prefill = %q, want /exports/ prefix", got)
	}

	for _, r := range "x.patch" {
		m, _ = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	var cmd tea.Cmd
	m, cmd = send(m, keyType(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter should dispatch the op")
	}
	if _, still := m.topLayer().(*applyPatchPopup); still {
		t.Fatal("popup should close on dispatch")
	}
}

// enter on an empty path is a no-op (popup stays, nothing dispatched).
func TestApplyPatchPopupEmptyPathNoop(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m, _ = send(m, applyPatchDirMsg{err: errTest})
	p, ok := m.topLayer().(*applyPatchPopup)
	if !ok || p.path.Value() != "" {
		t.Fatalf("resolve error should still open the popup with empty prefill (layer %T)", m.topLayer())
	}
	var cmd tea.Cmd
	m, cmd = send(m, keyType(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("enter on empty path must not dispatch")
	}
	if _, still := m.topLayer().(*applyPatchPopup); !still {
		t.Fatal("popup should stay open")
	}
}

// esc closes without dispatching.
func TestApplyPatchPopupEscCancels(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m, _ = send(m, applyPatchDirMsg{dir: "/exports"})
	m, _ = send(m, keyType(tea.KeyEsc))
	if _, still := m.topLayer().(*applyPatchPopup); still {
		t.Fatal("esc should close the popup")
	}
}
