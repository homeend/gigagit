package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/observ"
)

func errorsMenuIndex(t *testing.T) int {
	t.Helper()
	for i := range settingsMenu {
		if settingsMenu[i] == settingsMenuErrors {
			return i
		}
	}
	t.Fatal("session-errors entry missing from settings menu")
	return -1
}

func TestSettingsErrorsLabelShowsCount(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m, _ := settingsModel(t)
	observ.ResetFailures()
	idx := errorsMenuIndex(t)

	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "none") {
		t.Fatalf("empty-state label should say 'none': %q", got)
	}
	observ.NoteFailure("op SmartPull", errors.New("git pull failed (exit 1): rejected"))
	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "1") {
		t.Fatalf("label should reflect count 1: %q", got)
	}
}

func TestSettingsErrorsViewerOpensAndCloses(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m, _ := settingsModel(t)
	observ.ResetFailures()
	observ.NoteFailure("op SmartPull", errors.New("git pull failed (exit 1): rejected"))

	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	// Select the Session errors row, then open it.
	layerOf[*settingsPopup](m).menuSel = errorsMenuIndex(t)
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)

	p := layerOf[*settingsPopup](m)
	if p == nil || !p.errorsView {
		t.Fatal("enter on the Session errors row should open the viewer")
	}
	// The source renders near the line start; the long detail is truncated by
	// the default cutoff display mode, so assert on the source, not its tail.
	if out := m.View(); !strings.Contains(out, "Session errors") || !strings.Contains(out, "SmartPull") {
		t.Fatalf("viewer should list the failure:\n%s", out)
	}
	// esc returns to the menu (does not close the popup).
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	p = layerOf[*settingsPopup](m)
	if p == nil || p.errorsView {
		t.Fatal("esc should return from the viewer to the menu, keeping the popup open")
	}
}

func TestSettingsErrorsEmptyState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m, _ := settingsModel(t)
	observ.ResetFailures()
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	layerOf[*settingsPopup](m).menuSel = errorsMenuIndex(t)
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if out := m.View(); !strings.Contains(out, "no errors this session") {
		t.Fatalf("empty viewer should show the empty state:\n%s", out)
	}
}
