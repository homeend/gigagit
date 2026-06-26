package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
)

func TestConfirmOpPopsModalDefaultNo(t *testing.T) {
	m := loadedModel(t) // build a loaded model; reuse the existing helper
	tm, _ := m.confirmOp(engine.SmartSwitch{Branch: "x"}, "Switch to x?")
	mm := tm.(Model)
	if mm.modal == nil {
		t.Fatal("confirmOp should pop a modal when confirmation is enabled")
	}
	if !mm.modal.confirm {
		t.Fatal("confirm modal must set confirm=true")
	}
	if got := mm.modal.req.Options; len(got) != 2 || got[0] != "Yes" || got[1] != "No" {
		t.Fatalf("options = %v, want [Yes No]", got)
	}
	if mm.modal.req.Options[mm.modal.sel] != "No" {
		t.Fatalf("default selection = %q, want No", mm.modal.req.Options[mm.modal.sel])
	}
}

func TestConfirmOpDisabledRunsDirectly(t *testing.T) {
	m := loadedModel(t)
	m.cfg.UI.DisableSlowOpConfirm = true
	tm, cmd := m.confirmOp(engine.SmartSwitch{Branch: "x"}, "Switch to x?")
	mm := tm.(Model)
	if mm.modal != nil {
		t.Fatal("disabled confirmation must not pop a modal")
	}
	if cmd == nil {
		t.Fatal("disabled confirmation must launch the op (non-nil cmd)")
	}
}

func TestConfirmModalKeys(t *testing.T) {
	key := func(s string) tea.KeyMsg {
		if len(s) == 1 {
			return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
		}
		switch s {
		case "enter":
			return tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			return tea.KeyMsg{Type: tea.KeyEsc}
		}
		return tea.KeyMsg{}
	}

	// enter on the default selection = No = no op launched.
	t.Run("enter defaults to No", func(t *testing.T) {
		m := loadedModel(t)
		tm, _ := m.confirmOp(engine.SmartSwitch{Branch: "x"}, "p?")
		tm, cmd := tm.(Model).Update(key("enter"))
		if tm.(Model).modal != nil {
			t.Fatal("enter should dismiss the modal")
		}
		if cmd != nil {
			t.Fatal("enter on default (No) must not launch the op")
		}
	})

	// y launches the op and dismisses.
	t.Run("y confirms", func(t *testing.T) {
		m := loadedModel(t)
		tm, _ := m.confirmOp(engine.SmartSwitch{Branch: "x"}, "p?")
		tm, cmd := tm.(Model).Update(key("y"))
		if tm.(Model).modal != nil {
			t.Fatal("y should dismiss the modal")
		}
		if cmd == nil {
			t.Fatal("y must launch the op")
		}
	})

	// n / esc dismiss without launching.
	for _, k := range []string{"n", "esc"} {
		t.Run(k+" cancels", func(t *testing.T) {
			m := loadedModel(t)
			tm, _ := m.confirmOp(engine.SmartSwitch{Branch: "x"}, "p?")
			tm, cmd := tm.(Model).Update(key(k))
			if tm.(Model).modal != nil {
				t.Fatalf("%s should dismiss the modal", k)
			}
			if cmd != nil {
				t.Fatalf("%s must not launch the op", k)
			}
		})
	}
}
