package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/theme"
)

// buildStyles(theme.Terminal) must reproduce every literal the TUI used before
// themes existed — the guarantee that `terminal` is byte-identical to today.
func TestBuildStylesTerminalPinsLegacyLiterals(t *testing.T) {
	t.Parallel()
	s := buildStyles(theme.Terminal)
	fg := func(st lipgloss.Style) string { return string(st.GetForeground().(lipgloss.Color)) }
	bg := func(st lipgloss.Style) string { return string(st.GetBackground().(lipgloss.Color)) }
	border := func(st lipgloss.Style) string { return string(st.GetBorderTopForeground().(lipgloss.Color)) }
	checks := []struct {
		name string
		got  string
		want string
	}{
		{"focusedPanel border", border(s.focusedPanel), "12"},
		{"bluredPanel border", border(s.bluredPanel), "240"},
		{"modalStyle border", border(s.modalStyle), "11"},
		{"statusErr fg", fg(s.statusErr), "15"},
		{"statusErr bg", bg(s.statusErr), "1"},
		{"error fg", fg(s.errorText), "9"},
		{"dim fg", fg(s.dim), "240"},
		{"tagDeco fg", fg(s.tagDeco), "220"},
		{"pickerLabel fg", fg(s.pickerLabel), "245"},
		{"messageBlock bg", bg(s.messageBlock), "236"},
		{"diffDel bg", bg(s.diffDelCell), "52"},
		{"diffAdd bg", bg(s.diffAddCell), "22"},
		{"diffEmph fg", fg(s.diffEmph), "231"},
		{"diffCursorRow bg", bg(s.diffCursorRow), "237"},
		{"diffAddCursor bg", bg(s.diffAddCursor), "28"},
		{"diffDelCursor bg", bg(s.diffDelCursor), "88"},
		{"diffGapCursor bg", bg(s.diffGapCursor), "237"},
		{"noteFrameUser fg", fg(s.noteFrameUser), "75"},
		{"noteFrameAgent fg", fg(s.noteFrameAgent), "141"},
		{"noteBody fg", fg(s.noteBody), "250"},
		{"field bg", bg(s.field), "236"},
		{"fieldCursor fg", fg(s.fieldCursor), "236"},
		{"fieldCursor bg", bg(s.fieldCursor), "250"},
		{"noticeHot fg", fg(s.noticeHot), "196"},
		{"noticeDim fg", fg(s.noticeDim), "124"},
		{"saveBanner fg", fg(s.saveBanner), "15"},
		{"saveBanner bg", bg(s.saveBanner), "22"},
		{"tooltip fg", fg(s.tooltip), "0"},
		{"tooltip bg", bg(s.tooltip), "11"},
		{"reviewHot fg", fg(s.reviewHot), "39"},
		{"reviewDim fg", fg(s.reviewDim), "31"},
		{"lane 0", string(s.lane(0)), "33"},
		{"lane 8 wraps", string(s.lane(8)), "208"},
		{"lane -1 clamps", string(s.lane(-1)), "33"},
		{"syntax keyword", s.syntaxColor(syntax.Keyword), "141"},
		{"syntax attr", s.syntaxColor(syntax.Attr), "180"},
		{"syntax plain", s.syntaxColor(syntax.Plain), ""},
		{"syntax name", s.syntaxColor(syntax.Name), ""},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if !s.selectedRow.GetReverse() {
		t.Error("selectedRow must stay Reverse(true)")
	}
	if bgc, fgc := s.frame(); bgc != "" || fgc != "" {
		t.Errorf("terminal frame = (%q, %q), want empty (no paint)", bgc, fgc)
	}

	// Bold is an attribute the fg/bg-only checks above can't see: a role that
	// silently gains or loses Bold(true) still passes every colour check.
	// Pin every role the legacy call sites carried bold on — and, just as
	// important, every role that must stay PLAIN (a caught regression:
	// saveBanner briefly gained Bold(true) here, which the fg/bg checks above
	// never noticed).
	boldChecks := []struct {
		name string
		got  bool
		want bool
	}{
		{"titleStyle bold", s.titleStyle.GetBold(), true},
		{"statusErr bold", s.statusErr.GetBold(), true},
		{"reviewHot bold", s.reviewHot.GetBold(), true},
		{"pickerLabel bold", s.pickerLabel.GetBold(), true},
		{"diffEmph bold", s.diffEmph.GetBold(), true},
		{"diffCursorNo bold", s.diffCursorNo.GetBold(), true},
		{"noteSummary bold", s.noteSummary.GetBold(), true},
		{"noticeHot bold", s.noticeHot.GetBold(), true},
		{"saveBanner bold", s.saveBanner.GetBold(), false},
		{"dim bold", s.dim.GetBold(), false},
		{"noteBody bold", s.noteBody.GetBold(), false},
		{"tooltip bold", s.tooltip.GetBold(), false},
		{"errorText bold", s.errorText.GetBold(), false},
		{"field bold", s.field.GetBold(), false},
		{"fieldCursor bold", s.fieldCursor.GetBold(), false},
		{"messageBlock bold", s.messageBlock.GetBold(), false},
		{"diffAddCell bold", s.diffAddCell.GetBold(), false},
		{"diffDelCell bold", s.diffDelCell.GetBold(), false},
		{"diffCursorRow bold", s.diffCursorRow.GetBold(), false},
	}
	for _, c := range boldChecks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	// Border SHAPE (rounded vs double), not just colour: a role that swapped
	// borders would still pass the border-colour checks above.
	if got := s.focusedPanel.GetBorderStyle(); got != lipgloss.RoundedBorder() {
		t.Errorf("focusedPanel border style = %+v, want RoundedBorder", got)
	}
	if got := s.bluredPanel.GetBorderStyle(); got != lipgloss.RoundedBorder() {
		t.Errorf("bluredPanel border style = %+v, want RoundedBorder", got)
	}
	if got := s.modalStyle.GetBorderStyle(); got != lipgloss.DoubleBorder() {
		t.Errorf("modalStyle border style = %+v, want DoubleBorder", got)
	}

	// Padding: pin Padding(0,1) on the panel frames and Padding(1,2) on the
	// modal styles (both use asymmetric top/left values a single "want"
	// number can't express, so check top and left separately).
	if got := s.focusedPanel.GetPaddingTop(); got != 0 {
		t.Errorf("focusedPanel padding top = %d, want 0", got)
	}
	if got := s.focusedPanel.GetPaddingLeft(); got != 1 {
		t.Errorf("focusedPanel padding left = %d, want 1", got)
	}
	if got := s.bluredPanel.GetPaddingTop(); got != 0 {
		t.Errorf("bluredPanel padding top = %d, want 0", got)
	}
	if got := s.bluredPanel.GetPaddingLeft(); got != 1 {
		t.Errorf("bluredPanel padding left = %d, want 1", got)
	}
	if got := s.modalStyle.GetPaddingTop(); got != 1 {
		t.Errorf("modalStyle padding top = %d, want 1", got)
	}
	if got := s.modalStyle.GetPaddingLeft(); got != 2 {
		t.Errorf("modalStyle padding left = %d, want 2", got)
	}

	// errModal = modalStyle.BorderForeground(ErrFg): DoubleBorder + Padding(1,2)
	// carry over from modalStyle, only the border colour changes.
	if got := border(s.errModal); got != "9" {
		t.Errorf("errModal border fg = %q, want 9", got)
	}
	if got := s.errModal.GetBorderStyle(); got != lipgloss.DoubleBorder() {
		t.Errorf("errModal border style = %+v, want DoubleBorder", got)
	}
	if got := s.errModal.GetPaddingTop(); got != 1 {
		t.Errorf("errModal padding top = %d, want 1", got)
	}
	if got := s.errModal.GetPaddingLeft(); got != 2 {
		t.Errorf("errModal padding left = %d, want 2", got)
	}
}

func TestBuildStylesDarkAppliesRoles(t *testing.T) {
	t.Parallel()
	s := buildStyles(theme.Dark)
	if got := string(s.focusedPanel.GetBorderTopForeground().(lipgloss.Color)); got != "#3B78FF" {
		t.Fatalf("dark focus border = %q", got)
	}
	if got := string(s.dim.GetForeground().(lipgloss.Color)); got != "#585858" {
		t.Fatalf("dark dim = %q", got)
	}
	// Unchanged 256-cube roles are kept verbatim.
	if got := string(s.diffAddCell.GetBackground().(lipgloss.Color)); got != "22" {
		t.Fatalf("dark diffAdd bg = %q, want 22", got)
	}
	if bgc, fgc := s.frame(); bgc != "#0C0C0C" || fgc != "#CCCCCC" {
		t.Fatalf("dark frame = (%q, %q)", bgc, fgc)
	}
	if got := string(s.statusErr.GetForeground().(lipgloss.Color)); got != "#F2F2F2" {
		t.Fatalf("dark statusErr fg = %q, want #F2F2F2", got)
	}
}

// Light's statusErr must use the dedicated StatusErrFg role, not the frame's
// SaveBannerFg (which under Light is the near-white frame bg on a pale ErrBg —
// unreadable).
func TestBuildStylesLightStatusErrReadable(t *testing.T) {
	t.Parallel()
	s := buildStyles(theme.Light)
	if got := string(s.statusErr.GetForeground().(lipgloss.Color)); got != "#33393F" {
		t.Fatalf("light statusErr fg = %q, want #33393F (StatusErrFg), not the frame bg", got)
	}
}

// A role left "" in a theme inherits the terminal literal.
func TestBuildStylesEmptyRoleInherits(t *testing.T) {
	t.Parallel()
	th := theme.Dark
	th.Dim = ""
	s := buildStyles(th)
	if got := string(s.dim.GetForeground().(lipgloss.Color)); got != "240" {
		t.Fatalf("empty Dim must inherit 240, got %q", got)
	}
}

// NOTE: serial (no t.Parallel) — setTheme swaps the process-global styles pointer.
func TestSetThemeSwapsAndRestores(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	setTheme(theme.Light)
	if activeTheme().Name != theme.NameLight {
		t.Fatalf("activeTheme = %q after setTheme(Light)", activeTheme().Name)
	}
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != "#8A8F8A" {
		t.Fatalf("st().dim after Light = %q", got)
	}
	setTheme(theme.Terminal)
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != "240" {
		t.Fatalf("st().dim after Terminal = %q", got)
	}
}
