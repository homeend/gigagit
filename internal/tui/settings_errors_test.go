package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

// TestSettingsErrorsViewerPathFullyVisible guards bug 1: the errors.log path
// must be shown in full (its basename reachable), not truncated with an ellipsis.
func TestSettingsErrorsViewerPathFullyVisible(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m, _ := settingsModel(t)
	observ.ResetFailures()
	observ.NoteFailure("op SmartPull", errors.New("boom"))

	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	layerOf[*settingsPopup](m).menuSel = errorsMenuIndex(t)
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)

	// The path's basename sits at the very end; truncation drops it. Wrapping
	// keeps it visible.
	if out := m.View(); !strings.Contains(out, "errors.log") {
		t.Fatalf("errors.log path basename should be visible, not truncated:\n%s", out)
	}
}

// TestSettingsErrorsViewerWrapRevealsDetail guards bug 2: pressing z once
// (cutoff -> wrap) must actually wrap a long detail onto a continuation line,
// revealing a tail token that does not fit on the first line.
func TestSettingsErrorsViewerWrapRevealsDetail(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m, _ := settingsModel(t)
	observ.ResetFailures()
	const tail = "SENTINEL_TAIL_TOKEN"
	observ.NoteFailure("op SmartPull",
		errors.New("git pull failed (exit 1): fatal: a long stderr message that overflows one line "+tail))

	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	layerOf[*settingsPopup](m).menuSel = errorsMenuIndex(t)
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)

	// Cutoff (default): the tail is truncated off the single line.
	if out := m.View(); strings.Contains(out, tail) {
		t.Fatalf("precondition: tail should be truncated in cutoff mode:\n%s", out)
	}
	// z -> wrap: the tail must now appear on a continuation line.
	u, _ = m.Update(keyMsg("ctrl+w"))
	m = u.(Model)
	if p := layerOf[*settingsPopup](m); p.mode != modeWrap {
		t.Fatalf("z should select wrap mode, got %v", p.mode)
	}
	if out := m.View(); !strings.Contains(out, tail) {
		t.Fatalf("wrap mode should reveal the detail tail on a continuation line:\n%s", out)
	}
}

// openErrorsView sizes the terminal (auto-maximize needs full > wide to have
// room to buy), opens Settings, and enters the Session errors view.
func openErrorsView(t *testing.T, m Model) Model {
	t.Helper()
	u, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = u.(Model)
	u, _ = m.Update(keyMsg(","))
	m = u.(Model)
	layerOf[*settingsPopup](m).menuSel = errorsMenuIndex(t)
	u, _ = m.Update(keyMsg("enter"))
	return u.(Model)
}

// TestSettingsErrorsAutoMaximizesForLongRows: a failure row too wide for even
// the wide errors box opens the viewer maximized (the pair-op
// autoMaxForContent precedent — decided once at entry), and esc restores the
// menu's normal size instead of leaving the small menu near-fullscreen.
func TestSettingsErrorsAutoMaximizesForLongRows(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m, _ := settingsModel(t)
	observ.ResetFailures()
	observ.NoteFailure("op Push",
		errors.New("git push failed (exit 128): fatal: "+strings.Repeat("wide-token-", 20)))
	m = openErrorsView(t, m)
	if p := layerOf[*settingsPopup](m); !p.maximized {
		t.Fatal("a row wider than the wide box should open the errors viewer maximized")
	}
	u, _ := m.Update(keyMsg("esc"))
	m = u.(Model)
	if p := layerOf[*settingsPopup](m); p.maximized {
		t.Fatal("esc after an auto-maximize should restore the menu's normal size")
	}
}

// Short rows fit the wide box — the viewer must NOT open maximized.
func TestSettingsErrorsStaysWideForShortRows(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m, _ := settingsModel(t)
	observ.ResetFailures()
	observ.NoteFailure("op X", errors.New("short"))
	m = openErrorsView(t, m)
	if p := layerOf[*settingsPopup](m); p.maximized {
		t.Fatal("short rows should keep the errors viewer at its wide (non-maximized) width")
	}
}

// A maximize the user chose BEFORE entering the errors view is theirs — esc
// out of the viewer must not shrink it.
func TestSettingsErrorsEscKeepsManualMaximize(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m, _ := settingsModel(t)
	observ.ResetFailures()
	observ.NoteFailure("op X", errors.New("short"))
	u, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = u.(Model)
	u, _ = m.Update(keyMsg(","))
	m = u.(Model)
	u, _ = m.Update(keyMsg("ctrl+t"))
	m = u.(Model)
	layerOf[*settingsPopup](m).menuSel = errorsMenuIndex(t)
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if p := layerOf[*settingsPopup](m); !p.maximized {
		t.Fatal("esc out of the errors view must not undo a manual ctrl+t maximize")
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
	out := m.View()
	if !strings.Contains(out, "no errors this session") {
		t.Fatalf("empty viewer should show the empty state:\n%s", out)
	}
	// Nothing to wrap/scroll with no entries: don't advertise z (the user's
	// complaint — z was advertised but inert in the empty state).
	if strings.Contains(out, "[ctrl+w] mode") {
		t.Fatalf("empty viewer must not advertise [ctrl+w] mode:\n%s", out)
	}
	if !strings.Contains(out, "[esc] back") {
		t.Fatalf("empty viewer should still offer [esc] back:\n%s", out)
	}
}

// TestSettingsErrorsZHintGatedOnTruncation: [ctrl+w] mode is advertised only when an
// entry is too long to fit (so wrap/scroll have something to reveal).
func TestSettingsErrorsZHintGatedOnTruncation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m, _ := settingsModel(t)
	observ.ResetFailures()
	// A short error that fits the wide popup on one line -> no [ctrl+w].
	observ.NoteFailure("op X", errors.New("short"))
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	layerOf[*settingsPopup](m).menuSel = errorsMenuIndex(t)
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if out := m.View(); strings.Contains(out, "[ctrl+w] mode") {
		t.Fatalf("a short, fully-visible entry must not advertise [ctrl+w] mode:\n%s", out)
	}

	// A very long error that overflows even the wide popup -> [ctrl+w] appears.
	observ.NoteFailure("op SmartPull",
		errors.New("git pull failed (exit 1): fatal: "+strings.Repeat("very-long-token-", 12)))
	if out := m.View(); !strings.Contains(out, "[ctrl+w] mode") {
		t.Fatalf("a truncated entry should advertise [ctrl+w] mode:\n%s", out)
	}
}
