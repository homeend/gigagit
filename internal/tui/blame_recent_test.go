package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/theme"
	"github.com/homeend/gigagit/internal/timespan"
)

// NOTE: TestBlameRecentPaintsTheRowDark and TestBlameRecentPaintsBoldOnTerminal
// call lipgloss.SetColorProfile / setTheme (process-global) and therefore do
// NOT call t.Parallel().

// mustFilter parses an age filter or fails the test.
func mustFilter(t *testing.T, s string) timespan.Filter {
	t.Helper()
	f, err := timespan.ParseFilter(s)
	if err != nil {
		t.Fatalf("ParseFilter(%q): %v", s, err)
	}
	return f
}

func TestLineMatches(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	day := 24 * time.Hour
	at := func(age time.Duration) model.BlameLine {
		return model.BlameLine{Hash: "a", Time: now.Add(-age).Unix()}
	}
	dirty := model.BlameLine{Hash: "", Time: 0}
	cases := []struct {
		name   string
		ln     model.BlameLine
		filter string
		want   bool
	}{
		// younger than (-)
		{"younger: just now", at(0), "-7d", true},
		{"younger: exact boundary is inclusive", at(7 * day), "-7d", true},
		{"younger: one second past the boundary", at(7*day + time.Second), "-7d", false},
		{"younger: a bare span is the - half", at(6 * day), "7d", true},
		{"younger: uncommitted has age 0", dirty, "-7d", true},
		{"younger: epoch is old", model.BlameLine{Hash: "a", Time: 0}, "-7d", false},
		// older than (+)
		{"older: just now", at(0), "+7d", false},
		{"older: exact boundary is inclusive", at(7 * day), "+7d", true},
		{"older: one second short of the boundary", at(7*day - time.Second), "+7d", false},
		{"older: ancient", at(400 * day), "+7d", true},
		{"older: epoch matches", model.BlameLine{Hash: "a", Time: 0}, "+7d", true},
		{"older: uncommitted never matches", dirty, "+7d", false},
		// between (+ and -)
		{"between: inside", at(3 * day), "+1d -7d", true},
		{"between: at the older bound", at(day), "+1d -7d", true},
		{"between: at the younger bound", at(7 * day), "+1d -7d", true},
		{"between: too young", at(day - time.Second), "+1d -7d", false},
		{"between: too old", at(7*day + time.Second), "+1d -7d", false},
		{"between: uncommitted never matches", dirty, "+1d -7d", false},
		{"between: a point range", at(7 * day), "+7d -7d", true},
	}
	for _, c := range cases {
		if got := lineMatches(c.ln, now, mustFilter(t, c.filter)); got != c.want {
			t.Errorf("%s: lineMatches(%s) = %v, want %v", c.name, c.filter, got, c.want)
		}
	}
	// The zero filter (no half set) matches nothing — an "on" state can never
	// carry it, but the predicate must not tint the world if it did.
	if lineMatches(at(0), now, timespan.Filter{}) {
		t.Error("the zero filter must match nothing")
	}
}

func TestBlameRecentSeed(t *testing.T) {
	t.Parallel()
	if got := blameRecentSeed(""); got != "7d" {
		t.Errorf("seed with no last = %q, want 7d", got)
	}
	if got := blameRecentSeed("+1d -7d"); got != "+1d -7d" {
		t.Errorf("seed keeps the last text, got %q", got)
	}
}

// typePopup drives keys into the popup on top of the blame view.
func typeRecentPopup(t *testing.T, m Model, keys ...tea.KeyMsg) Model {
	t.Helper()
	for _, k := range keys {
		p, ok := m.topLayer().(*blameRecentPopup)
		if !ok {
			t.Fatalf("the age popup is not on top before %v", k)
		}
		m, _ = p.update(m, k)
	}
	return m
}

func runesOf(s string) []tea.KeyMsg {
	var out []tea.KeyMsg
	for _, r := range s {
		if r == ' ' {
			out = append(out, tea.KeyMsg{Type: tea.KeySpace})
			continue
		}
		out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return out
}

// clearField backspaces the prefilled text away.
func clearField(n int) []tea.KeyMsg {
	out := make([]tea.KeyMsg, n)
	for i := range out {
		out[i] = tea.KeyMsg{Type: tea.KeyBackspace}
	}
	return out
}

// d opens the age popup prefilled with the default; enter with a good filter
// turns the highlight on for THIS view, remembers the text on the Model and
// closes the popup.
func TestBlameRecentKeyDOpensPopupAndEnterApplies(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m, _ = b.update(m, keyMsg("d"))
	p, ok := m.topLayer().(*blameRecentPopup)
	if !ok {
		t.Fatalf("d must push the age popup, top = %T", m.topLayer())
	}
	if got := p.input.Value(); got != "7d" {
		t.Fatalf("the popup must prefill the default, got %q", got)
	}
	keys := append(clearField(2), runesOf("+1d -7d")...)
	keys = append(keys, keyMsg("enter"))
	m = typeRecentPopup(t, m, keys...)
	want := blameRecent{on: true, f: mustFilter(t, "+1d -7d")}
	if b.recent != want {
		t.Errorf("view recent = %+v, want %+v", b.recent, want)
	}
	if m.blameRecentLast != "+1d -7d" {
		t.Errorf("blameRecentLast = %q, want the submitted text", m.blameRecentLast)
	}
	if layerOf[*blameRecentPopup](m) != nil {
		t.Error("a good filter must close the popup")
	}
	if _, ok := m.topLayer().(*blameView); !ok {
		t.Errorf("closing the popup must reveal the blame view, top = %T", m.topLayer())
	}
	// Reopening prefills the last text.
	m, _ = b.update(m, keyMsg("d"))
	if p := m.topLayer().(*blameRecentPopup); p.input.Value() != "+1d -7d" {
		t.Errorf("reopen must prefill the last text, got %q", p.input.Value())
	}
}

// The highlight is OFF whenever blame opens: closing the view and pushing a
// fresh one starts off, while the last text still seeds the dialog.
func TestBlameRecentOffOnOpenKeepsLastText(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m, _ = b.update(m, keyMsg("d"))
	keys := append(clearField(2), runesOf("+1d -7d")...)
	keys = append(keys, keyMsg("enter"))
	m = typeRecentPopup(t, m, keys...)
	if !b.recent.on {
		t.Fatal("precondition: the highlight is on after enter")
	}
	m = m.popLayer() // close blame
	b2 := blameFixture()
	m = m.pushLayer(b2) // …and reopen it
	if b2.recent.on {
		t.Error("a fresh blame view must start with the highlight off")
	}
	if b2.recent != (blameRecent{}) {
		t.Errorf("a fresh blame view carries no filter, got %+v", b2.recent)
	}
	if m.blameRecentLast != "+1d -7d" {
		t.Errorf("the last text must survive closing blame, got %q", m.blameRecentLast)
	}
	m, _ = b2.update(m, keyMsg("d"))
	p, ok := m.topLayer().(*blameRecentPopup)
	if !ok {
		t.Fatalf("d must push the age popup, top = %T", m.topLayer())
	}
	if p.input.Value() != "+1d -7d" {
		t.Errorf("the dialog must prefill the last text, got %q", p.input.Value())
	}
	// Enter applies to the NEW view.
	m = typeRecentPopup(t, m, keyMsg("enter"))
	if want := (blameRecent{on: true, f: mustFilter(t, "+1d -7d")}); b2.recent != want {
		t.Errorf("enter must apply to the reopened view: %+v, want %+v", b2.recent, want)
	}
}

// The first typed rune REPLACES the prefill (the web prompt selects its value
// for the same reason): "7d" + typed "3d" is 3d, never 7d3d. Backspace first
// keeps the prefill editable in place.
func TestBlameRecentFirstRuneReplacesPrefill(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m, _ = b.update(m, keyMsg("d"))
	keys := append(runesOf("3d"), keyMsg("enter"))
	m = typeRecentPopup(t, m, keys...)
	if want := (blameRecent{on: true, f: mustFilter(t, "3d")}); b.recent != want {
		t.Errorf("typing over the prefill: recent = %+v, want %+v", b.recent, want)
	}
	if m.blameRecentLast != "3d" {
		t.Errorf("blameRecentLast = %q, want 3d", m.blameRecentLast)
	}
	// Editing the prefill in place (backspace first) keeps the rest of it.
	m, _ = b.update(m, keyMsg("d"))
	keys = append(clearField(1), runesOf("h")...)
	keys = append(keys, keyMsg("enter"))
	m = typeRecentPopup(t, m, keys...)
	if want := (blameRecent{on: true, f: mustFilter(t, "3h")}); b.recent != want {
		t.Errorf("editing the prefill in place: recent = %+v, want %+v", b.recent, want)
	}
	if m.blameRecentLast != "3h" {
		t.Errorf("blameRecentLast = %q, want 3h", m.blameRecentLast)
	}
}

// An empty range ("+7d -1d": older than a week AND younger than a day) keeps
// the popup open with an inline error; the state is untouched.
func TestBlameRecentEmptyRangeKeepsPopupWithError(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m, _ = b.update(m, keyMsg("d"))
	keys := append(clearField(2), runesOf("+7d -1d")...)
	keys = append(keys, keyMsg("enter"))
	m = typeRecentPopup(t, m, keys...)
	p, ok := m.topLayer().(*blameRecentPopup)
	if !ok {
		t.Fatalf("an empty range must keep the popup open, top = %T", m.topLayer())
	}
	if p.err != i18n.T("not an age filter: %s", "+7d -1d") {
		t.Errorf("err = %q, want the not-an-age-filter line", p.err)
	}
	if !strings.Contains(p.box(m), "not an age filter: +7d -1d") {
		t.Errorf("the box must show the error:\n%s", p.box(m))
	}
	if b.recent.on {
		t.Error("a bad filter must not turn the highlight on")
	}
	if m.blameRecentLast != "" {
		t.Errorf("a bad filter must not be remembered, got %q", m.blameRecentLast)
	}
	// Editing clears the error.
	m = typeRecentPopup(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if p.err != "" {
		t.Errorf("editing must clear the error, got %q", p.err)
	}
}

// Junk (a token error inside a half) is the same inline error.
func TestBlameRecentJunkKeepsPopupWithError(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m, _ = b.update(m, keyMsg("d"))
	keys := append(clearField(2), runesOf("junk")...)
	keys = append(keys, keyMsg("enter"))
	m = typeRecentPopup(t, m, keys...)
	p, ok := m.topLayer().(*blameRecentPopup)
	if !ok {
		t.Fatalf("junk must keep the popup open, top = %T", m.topLayer())
	}
	if p.err != i18n.T("not an age filter: %s", "junk") {
		t.Errorf("err = %q, want the not-an-age-filter line", p.err)
	}
	if b.recent.on {
		t.Error("junk must not turn the highlight on")
	}
}

// Esc leaves the state exactly as it was, even after typing.
func TestBlameRecentEscLeavesStateUntouched(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.recent = blameRecent{on: true, f: mustFilter(t, "1h")}
	m.blameRecentLast = "1h"
	m, _ = b.update(m, keyMsg("d"))
	keys := append(runesOf("9"), keyMsg("esc"))
	m = typeRecentPopup(t, m, keys...)
	if layerOf[*blameRecentPopup](m) != nil {
		t.Error("esc must close the popup")
	}
	if want := (blameRecent{on: true, f: mustFilter(t, "1h")}); b.recent != want {
		t.Errorf("esc must not change the view state: %+v, want %+v", b.recent, want)
	}
	if m.blameRecentLast != "1h" {
		t.Errorf("esc must not change the last text, got %q", m.blameRecentLast)
	}
}

// D turns the highlight off but the last text still seeds the next d.
func TestBlameRecentShiftDClearsButKeepsLast(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.recent = blameRecent{on: true, f: mustFilter(t, "1h")}
	m.blameRecentLast = "1h"
	m, _ = b.update(m, keyMsg("D"))
	if b.recent.on {
		t.Error("D must turn the highlight off")
	}
	if m.blameRecentLast != "1h" {
		t.Errorf("D must keep the last text, got %q", m.blameRecentLast)
	}
	if layerOf[*blameRecentPopup](m) != nil {
		t.Error("D must not open the popup")
	}
}

// The popup swallows a global key — p while open must not leak to the panels.
func TestBlameRecentPopupSwallowsKeys(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m, _ = b.update(m, keyMsg("d"))
	m = typeRecentPopup(t, m, key("p"))
	if _, ok := m.topLayer().(*blameRecentPopup); !ok {
		t.Fatalf("p must stay inside the popup, top = %T", m.topLayer())
	}
	if m.running {
		t.Error("p inside the popup must start no op")
	}
}

// recentFixture is a 3-line blame: line 0 changed an hour ago (its own
// block, so its gutter is painted), lines 1–2 are ancient; the cursor sits on
// the old line 1.
func recentFixture(now time.Time) *blameView {
	b := &blameView{
		ctx: navContext{path: "a.go"},
		lines: []model.BlameLine{
			{Hash: "bbbbbbb", Author: "Bob", Time: now.Add(-time.Hour).Unix(), LineNo: 1, Content: "fresh line"},
			{Hash: "aaaaaaa", Author: "Ada", Time: 1, LineNo: 2, Content: "old line one"},
			{Hash: "aaaaaaa", Author: "Ada", Time: 1, LineNo: 3, Content: "old line two"},
		},
		sel: 1,
	}
	b.blocks = groupBlame(b.lines)
	return b
}

// Under the Dark theme the matching row wears the tint background on the
// gutter AND the code; the other rows do not; the cursor row keeps its
// reverse video. A "+" filter flips it: the OLD row is tinted, the fresh one
// is not.
func TestBlameRecentPaintsTheRowDark(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Dark)

	now := time.Now()
	// 120 wide: the footer hint (with [d] age at its end) must not be clipped.
	m := Model{width: 120, height: 30}
	b := recentFixture(now)
	b.recent = blameRecent{on: true, f: mustFilter(t, "7d")}
	out := b.render(m, "")

	fresh := lineWith(out, "fresh line")
	if fresh == "" {
		t.Fatalf("the fresh line is missing:\n%s", out)
	}
	tint := sgrBefore(st().blameRecentStyle(lipgloss.NewStyle()).Render("x"), "x")
	if len(tint) == 0 || !strings.Contains(st().blameRecentStyle(lipgloss.NewStyle()).Render("x"), "48;2;") {
		t.Fatalf("the Dark theme must give a true-colour background tint, oracle %v", tint)
	}
	if got := sgrBefore(fresh, "bbbbbbb"); !subsetOf(tint, got) {
		t.Errorf("the gutter must wear the tint, params %v: %q", got, fresh)
	}
	if got := sgrBefore(fresh, "fresh line"); !subsetOf(tint, got) {
		t.Errorf("the code must wear the tint, params %v: %q", got, fresh)
	}
	cursor := lineWith(out, "old line one")
	if !strings.Contains(cursor, "\x1b[7m") {
		t.Errorf("the cursor row must keep its reverse video: %q", cursor)
	}
	if strings.Contains(cursor, "48;2;") {
		t.Errorf("the cursor row must not wear the tint: %q", cursor)
	}
	if old := lineWith(out, "old line two"); strings.Contains(old, "48;2;") {
		t.Errorf("an old row must not wear the tint: %q", old)
	}
	// The header carries the badge (canonical signed form), the footer the hint.
	if !strings.Contains(out, "-7d") {
		t.Errorf("the header must show the -7d badge:\n%s", out)
	}
	if strings.Contains(out, "≤") {
		t.Errorf("the badge no longer carries a ≤ prefix:\n%s", out)
	}
	if !strings.Contains(out, "[d] age") {
		t.Errorf("the footer must advertise [d] age:\n%s", out)
	}

	// An older-than filter tints the OLD row (not the cursor row) and leaves
	// the fresh one alone.
	b.recent = blameRecent{on: true, f: mustFilter(t, "+7d")}
	older := b.render(m, "")
	if fresh := lineWith(older, "fresh line"); strings.Contains(fresh, "48;2;") {
		t.Errorf("under +7d the fresh row must not wear the tint: %q", fresh)
	}
	if old := lineWith(older, "old line two"); !subsetOf(tint, sgrBefore(old, "old line two")) {
		t.Errorf("under +7d the old row must wear the tint: %q", old)
	}
	if cursor := lineWith(older, "old line one"); !strings.Contains(cursor, "\x1b[7m") || strings.Contains(cursor, "48;2;") {
		t.Errorf("under +7d the cursor row keeps its reverse video and no tint: %q", cursor)
	}
	if !strings.Contains(older, "+7d") {
		t.Errorf("the header must show the +7d badge:\n%s", older)
	}

	// A range badge prints both halves, older first.
	b.recent = blameRecent{on: true, f: mustFilter(t, "-7d +1d")}
	if rng := b.render(m, ""); !strings.Contains(rng, "+1d -7d") {
		t.Errorf("the header must show the +1d -7d badge:\n%s", rng)
	}

	// Off: nothing tinted, no badge.
	b.recent.on = false
	off := b.render(m, "")
	if strings.Contains(off, "48;2;") || strings.Contains(off, "-7d") || strings.Contains(off, "+1d") {
		t.Errorf("with the highlight off nothing may be tinted or badged:\n%q", off)
	}
}

// The Terminal theme has no tint role: the matching row goes bold instead,
// and the tint never appears.
func TestBlameRecentPaintsBoldOnTerminal(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Terminal)

	now := time.Now()
	m := Model{width: 100, height: 30}
	b := recentFixture(now)
	b.recent = blameRecent{on: true, f: mustFilter(t, "7d")}
	out := b.render(m, "")
	fresh := lineWith(out, "fresh line")
	if fresh == "" {
		t.Fatalf("the fresh line is missing:\n%s", out)
	}
	if !sgrBefore(fresh, "bbbbbbb")["1"] {
		t.Errorf("the gutter must be bold under the Terminal theme: %q", fresh)
	}
	if !sgrBefore(fresh, "fresh line")["1"] {
		t.Errorf("the code must be bold under the Terminal theme: %q", fresh)
	}
	if strings.Contains(out, "48;2;") {
		t.Errorf("the Terminal theme must not paint a background: %q", out)
	}
	if !strings.Contains(lineWith(out, "old line one"), "\x1b[7m") {
		t.Errorf("the cursor row must keep its reverse video:\n%q", out)
	}
	if old := lineWith(out, "old line two"); sgrBefore(old, "old line two")["1"] {
		t.Errorf("an old row must not be bold: %q", old)
	}
}

// With both a search and the highlight on, the header slot reads
// "-7d · /q  n/m".
func TestBlameRecentBadgeSharesSlotWithSearch(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	b.recent = blameRecent{on: true, f: mustFilter(t, "7d")}
	m = typeBlame(m, b, "/", "m", "a", "i", "n", "enter")
	head := strings.SplitN(b.render(m, ""), "\n", 2)[0]
	if !strings.Contains(head, "-7d · /main") {
		t.Errorf("header = %q, want the -7d · /main… badge", head)
	}
}
