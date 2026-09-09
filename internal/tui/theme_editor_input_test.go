package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/theme"
)

// Regression coverage for the "backspace looks stuck under WSL+tmux" report:
// deletion was always functionally correct, but every keystroke — including
// one that changes nothing visible — forced a full-screen tea.ClearScreen.
// These tests drive the field one key at a time and assert both the buffer
// AND the rendered row after every key, plus the returned command.

func startDimEditor(t *testing.T, name, prefill string) (Model, *themeEditorPopup) {
	t.Helper()
	m, p := themeEditorModel(t, name)
	p.sel = slices.IndexFunc(p.roles, func(r theme.RoleRef) bool { return r.Key == "dim" })
	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	if !p.editing {
		t.Fatal("enter must start editing")
	}
	p.field = newTextField(prefill)
	return m, p
}

// theRowShowsField asserts that the row under the cursor renders exactly the
// field's current value: the editing value cell is laid out as
// p.field.Value()+"█" in one contiguous run (see themeEditorPopup.rows), so
// this cannot false-positive against some OTHER row's colour text the way a
// bare substring check on the whole box could (dark's own bg is "#0C0C0C").
func theRowShowsField(t *testing.T, p *themeEditorPopup, m Model, step string) {
	t.Helper()
	want := p.field.Value() + "█"
	if !strings.Contains(p.box(m), want) {
		t.Fatalf("%s: row does not show %q:\n%s", step, want, p.box(m))
	}
}

func TestThemeEditorBackspaceDeletesOneRuneAtATime(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := startDimEditor(t, "dark", "#0C0C0C")

	want := []string{"#0C0C0", "#0C0C", "#0C0", "#0C", "#0", "#", ""}
	for i, w := range want {
		um, _ := m.Update(keyMsg("backspace"))
		m = um.(Model)
		if got := p.field.Value(); got != w {
			t.Fatalf("after backspace #%d: field = %q, want %q", i+1, got, w)
		}
		theRowShowsField(t, p, m, fmt.Sprintf("after backspace #%d", i+1))
	}
}

func TestThemeEditorCtrlHDeletesOneRuneAtATime(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := startDimEditor(t, "dark", "#0C0C0C")

	want := []string{"#0C0C0", "#0C0C", "#0C0", "#0C", "#0", "#", ""}
	for i, w := range want {
		um, _ := m.Update(keyMsg("ctrl+h"))
		m = um.(Model)
		if got := p.field.Value(); got != w {
			t.Fatalf("after ctrl+h #%d: field = %q, want %q", i+1, got, w)
		}
		theRowShowsField(t, p, m, fmt.Sprintf("after ctrl+h #%d", i+1))
	}
}

// A burst of backspaces to empty, then a single multi-rune paste-like
// KeyRunes message (bubbletea coalesces fast typing this way), must land a
// valid colour and preview it.
func TestThemeEditorBackspaceBurstThenPasteRetypes(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := startDimEditor(t, "dark", "#0C0C0C")

	for i := 0; i < 7; i++ {
		um, _ := m.Update(keyMsg("backspace"))
		m = um.(Model)
	}
	if p.field.Value() != "" {
		t.Fatalf("field after 7 backspaces = %q, want empty", p.field.Value())
	}

	um, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("#ff5f00")})
	m = um.(Model)
	if p.field.Value() != "#ff5f00" {
		t.Fatalf("field after retyping = %q", p.field.Value())
	}
	if p.statusErr {
		t.Fatalf("status is an error after a valid retype: %q", p.status)
	}
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != "#ff5f00" {
		t.Fatalf("preview did not apply the retyped colour, got %q", got)
	}
}

// --- Input cap (7 runes, the length of "#rrggbb") ---

func TestThemeEditorCapsInputOneRuneAtATime(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := startDimEditor(t, "dark", "")

	for _, r := range "123456789" { // 9 characters, one KeyRunes each
		um, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = um.(Model)
	}
	if got := p.field.Value(); got != "1234567" {
		t.Fatalf("field = %q, want the first 7 runes", got)
	}
}

func TestThemeEditorCapsInputSingleMultiRuneMessage(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := startDimEditor(t, "dark", "")

	um, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("#ff5f00zz")})
	m = um.(Model)
	if got := p.field.Value(); got != "#ff5f00" {
		t.Fatalf("field = %q, want truncated to 7 runes", got)
	}
}

func TestThemeEditorCapDropsSpaceAtLimit(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := startDimEditor(t, "dark", "#ff5f00") // already at the 7-rune cap

	um, _ := m.Update(keyMsg("space"))
	m = um.(Model)
	if got := p.field.Value(); got != "#ff5f00" {
		t.Fatalf("a space at the cap must be dropped, field = %q", got)
	}
}

// No accepted colour form ever contains a space, so it is dropped
// unconditionally — not only when it would exceed the cap.
func TestThemeEditorSpaceIsAlwaysDropped(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := startDimEditor(t, "dark", "#ff") // well under the cap

	um, _ := m.Update(keyMsg("space"))
	m = um.(Model)
	if got := p.field.Value(); got != "#ff" {
		t.Fatalf("a space must never be inserted, field = %q", got)
	}
}

// A paste with an embedded space must land with the space stripped, not just
// truncated at the cap.
func TestThemeEditorPasteStripsEmbeddedSpace(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := startDimEditor(t, "dark", "")

	um, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("#ff 5f00")})
	m = um.(Model)
	if got := p.field.Value(); got != "#ff5f00" {
		t.Fatalf("field = %q, want the space stripped (and the rest still fitting the cap)", got)
	}
}

// --- Skip the full repaint when nothing visible would change ---

func TestThemeEditorInvalidKeystrokeSkipsRepaintOnSecondTime(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := startDimEditor(t, "dark", "")

	um, cmd1 := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = um.(Model)
	if !p.statusErr {
		t.Fatalf("first invalid keystroke must set the error status, got %q", p.status)
	}
	_ = cmd1

	um, cmd2 := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = um.(Model)
	if !p.statusErr {
		t.Fatalf("second invalid keystroke must still be an error, got %q", p.status)
	}
	if cmd2 != nil {
		t.Fatal("a second consecutive invalid keystroke must not repaint (cmd must be nil)")
	}
}

func TestThemeEditorInvalidAfterValidClearsOnceThenSkips(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := startDimEditor(t, "dark", "")

	// A valid colour changes the active theme, so it MUST clear.
	um, cmdValid := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")})
	m = um.(Model)
	if p.statusErr {
		t.Fatalf("a valid colour must not be an error: %q", p.status)
	}
	if cmdValid == nil {
		t.Fatal("a valid colour that changes the active theme must repaint")
	}

	// Breaking it invalid reverts the theme — that revert IS a change, so it
	// clears exactly once.
	um, cmdInvalid1 := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = um.(Model)
	if !p.statusErr {
		t.Fatal("the field is now invalid")
	}
	if cmdInvalid1 == nil {
		t.Fatal("reverting away from an applied preview must repaint once")
	}

	// A further invalid keystroke changes nothing more: no repaint.
	um, cmdInvalid2 := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = um.(Model)
	if !p.statusErr {
		t.Fatal("still invalid")
	}
	if cmdInvalid2 != nil {
		t.Fatal("a further invalid keystroke after the revert must not repaint")
	}
}

// Two invalid keystrokes right after opening the editor (nothing was ever
// previewed) never need to repaint at all — the screen was already at
// previewBase.
func TestThemeEditorTwoInvalidKeystrokesFromFreshOpen(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := startDimEditor(t, "dark", "")

	um, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = um.(Model)
	um, cmd2 := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = um.(Model)
	if !p.statusErr {
		t.Fatal("expected an error status")
	}
	if cmd2 != nil {
		t.Fatal("second invalid keystroke must return a nil cmd")
	}
}

// --- Normalised-value status + save ---

func TestThemeEditorPreviewShowsNormalisedFormWhenDifferent(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := startDimEditor(t, "dark", "")

	um, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")})
	m = um.(Model)
	if p.statusErr {
		t.Fatalf("abc should normalise to a valid colour, status = %q", p.status)
	}
	if !strings.Contains(p.status, "abc") || !strings.Contains(p.status, "#aabbcc") {
		t.Fatalf("status must show typed → normalised, got %q", p.status)
	}
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != "#aabbcc" {
		t.Fatalf("the NORMALISED value must be previewed, got %q", got)
	}
}

func TestThemeEditorSaveWritesNormalisedValue(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := startDimEditor(t, "dark", "")

	um, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")})
	m = um.(Model)
	um, _ = m.Update(keyMsg("enter"))
	m = um.(Model)
	if p.editing {
		t.Fatal("enter on a valid (normalisable) value must save")
	}
	if p.global.Dim != "#aabbcc" {
		t.Fatalf("saved override = %q, want the normalised form", p.global.Dim)
	}
}

// --- On-screen accepted-forms help block (editing mode only) ---

func TestThemeEditorShowsAcceptedFormsHelpWhileEditing(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")
	p.sel = slices.IndexFunc(p.roles, func(r theme.RoleRef) bool { return r.Key == "dim" })

	browseBody := p.box(m)
	if strings.Contains(browseBody, "Colour forms:") {
		t.Fatal("the accepted-forms help must not show in browse mode")
	}

	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	if !p.editing {
		t.Fatal("enter must start editing")
	}
	editBody := p.box(m)
	if !strings.Contains(editBody, "#rrggbb") || !strings.Contains(editBody, "rgb") {
		t.Fatalf("help block line 1 missing:\n%s", editBody)
	}
	if !strings.Contains(editBody, "0–255") || !strings.Contains(editBody, "0208") {
		t.Fatalf("help block line 2 missing:\n%s", editBody)
	}
	if !strings.Contains(editBody, "[enter] save") || !strings.Contains(editBody, "[esc] revert") {
		t.Fatalf("footer save/revert hints missing:\n%s", editBody)
	}

	um, _ = m.Update(keyMsg("esc"))
	m = um.(Model)
	if p.editing {
		t.Fatal("esc must leave editing")
	}
	afterEsc := p.box(m)
	if strings.Contains(afterEsc, "Colour forms:") {
		t.Fatal("the accepted-forms help must disappear once editing ends")
	}
}

// The two-line accepted-forms help block must not grow the popup at all: the
// list window gives up exactly those two rows of its own budget, so the
// footer is never pushed further off a short terminal by entering edit mode.
func TestThemeEditorEditingHeightMatchesBrowsing(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")
	m.width, m.height = 80, 24
	p.sel = slices.IndexFunc(p.roles, func(r theme.RoleRef) bool { return r.Key == "dim" })

	browseH := lipgloss.Height(p.box(m))
	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	editBody := p.box(m)
	editH := lipgloss.Height(editBody)
	if editH != browseH {
		t.Fatalf("editing box height %d != browse box height %d — the footer may be pushed further off", editH, browseH)
	}
	if !strings.Contains(editBody, "[enter] save") || !strings.Contains(editBody, "[esc] revert") {
		t.Fatalf("the footer must still be present, not clipped off:\n%s", editBody)
	}
}
