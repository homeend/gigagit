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
	u, _ = m.Update(keyMsg("z"))
	m = u.(Model)
	if p := layerOf[*settingsPopup](m); p.mode != modeWrap {
		t.Fatalf("z should select wrap mode, got %v", p.mode)
	}
	if out := m.View(); !strings.Contains(out, tail) {
		t.Fatalf("wrap mode should reveal the detail tail on a continuation line:\n%s", out)
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
