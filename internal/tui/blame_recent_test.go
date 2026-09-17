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
)

// NOTE: TestBlameRecentPaintsTheRowDark and TestBlameRecentPaintsBoldOnTerminal
// call lipgloss.SetColorProfile / setTheme (process-global) and therefore do
// NOT call t.Parallel().

func TestLineRecent(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	span := 7 * 24 * time.Hour
	cases := []struct {
		name string
		ln   model.BlameLine
		want bool
	}{
		{"just now", model.BlameLine{Hash: "a", Time: now.Unix()}, true},
		{"exact boundary is inclusive", model.BlameLine{Hash: "a", Time: now.Add(-span).Unix()}, true},
		{"one second past the boundary", model.BlameLine{Hash: "a", Time: now.Add(-span - time.Second).Unix()}, false},
		{"uncommitted is always recent", model.BlameLine{Hash: "", Time: 0}, true},
		{"epoch is old", model.BlameLine{Hash: "a", Time: 0}, false},
	}
	for _, c := range cases {
		if got := lineRecent(c.ln, now, span); got != c.want {
			t.Errorf("%s: lineRecent = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBlameRecentSeedAndBadge(t *testing.T) {
	t.Parallel()
	if got := blameRecentSeed(blameRecent{}); got != "7d" {
		t.Errorf("seed with no last = %q, want 7d", got)
	}
	if got := blameRecentSeed(blameRecent{last: "36h"}); got != "36h" {
		t.Errorf("seed keeps the last text, got %q", got)
	}
	if got := blameRecentBadge(27*time.Hour + 5*time.Minute); got != "≤1d3h5m" {
		t.Errorf("badge = %q, want ≤1d3h5m", got)
	}
}

// typePopup drives keys into the popup on top of the blame view.
func typeRecentPopup(t *testing.T, m Model, keys ...tea.KeyMsg) Model {
	t.Helper()
	for _, k := range keys {
		p, ok := m.topLayer().(*blameRecentPopup)
		if !ok {
			t.Fatalf("the span popup is not on top before %v", k)
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

// d opens the span popup prefilled with the default; enter with a good span
// turns the highlight on and closes the popup.
func TestBlameRecentKeyDOpensPopupAndEnterApplies(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m, _ = b.update(m, keyMsg("d"))
	p, ok := m.topLayer().(*blameRecentPopup)
	if !ok {
		t.Fatalf("d must push the span popup, top = %T", m.topLayer())
	}
	if got := p.input.Value(); got != "7d" {
		t.Fatalf("the popup must prefill the default span, got %q", got)
	}
	keys := append(clearField(2), runesOf("1d 3h")...)
	keys = append(keys, keyMsg("enter"))
	m = typeRecentPopup(t, m, keys...)
	want := blameRecent{on: true, span: 27 * time.Hour, last: "1d 3h"}
	if m.blameRecent != want {
		t.Errorf("blameRecent = %+v, want %+v", m.blameRecent, want)
	}
	if layerOf[*blameRecentPopup](m) != nil {
		t.Error("a good span must close the popup")
	}
	if _, ok := m.topLayer().(*blameView); !ok {
		t.Errorf("closing the popup must reveal the blame view, top = %T", m.topLayer())
	}
	// Reopening prefills the last span.
	m, _ = b.update(m, keyMsg("d"))
	if p := m.topLayer().(*blameRecentPopup); p.input.Value() != "1d 3h" {
		t.Errorf("reopen must prefill the last span, got %q", p.input.Value())
	}
}

// Junk keeps the popup open with an inline error; the state is untouched.
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
	if p.err != i18n.T("not a time span: %s", "junk") {
		t.Errorf("err = %q, want the not-a-time-span line", p.err)
	}
	if !strings.Contains(p.box(m), "not a time span: junk") {
		t.Errorf("the box must show the error:\n%s", p.box(m))
	}
	if m.blameRecent.on {
		t.Error("junk must not turn the highlight on")
	}
	// Editing clears the error.
	m = typeRecentPopup(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if p.err != "" {
		t.Errorf("editing must clear the error, got %q", p.err)
	}
}

// Esc leaves the state exactly as it was, even after typing.
func TestBlameRecentEscLeavesStateUntouched(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m.blameRecent = blameRecent{on: true, span: time.Hour, last: "1h"}
	m, _ = b.update(m, keyMsg("d"))
	keys := append(runesOf("9"), keyMsg("esc"))
	m = typeRecentPopup(t, m, keys...)
	if layerOf[*blameRecentPopup](m) != nil {
		t.Error("esc must close the popup")
	}
	if want := (blameRecent{on: true, span: time.Hour, last: "1h"}); m.blameRecent != want {
		t.Errorf("esc must not change the state: %+v, want %+v", m.blameRecent, want)
	}
}

// D turns the highlight off but remembers the span for the next d.
func TestBlameRecentShiftDClearsButKeepsLast(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m.blameRecent = blameRecent{on: true, span: time.Hour, last: "1h"}
	m, _ = b.update(m, keyMsg("D"))
	if m.blameRecent.on {
		t.Error("D must turn the highlight off")
	}
	if m.blameRecent.last != "1h" {
		t.Errorf("D must keep the last span, got %q", m.blameRecent.last)
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
		t.Error("p inside the popup must not start an op")
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

// Under the Dark theme the recent row wears the tint background on the gutter
// AND the code; the old rows do not; the cursor row keeps its reverse video.
func TestBlameRecentPaintsTheRowDark(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Dark)

	now := time.Now()
	m := Model{width: 100, height: 30, blameRecent: blameRecent{on: true, span: 7 * 24 * time.Hour}}
	b := recentFixture(now)
	out := b.render(m, "")

	fresh := lineWith(out, "fresh line")
	if fresh == "" {
		t.Fatalf("the recent line is missing:\n%s", out)
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
	// The header carries the badge, the footer the hint.
	if !strings.Contains(out, "≤7d") {
		t.Errorf("the header must show the ≤7d badge:\n%s", out)
	}
	if !strings.Contains(out, "[d] recent") {
		t.Errorf("the footer must advertise [d] recent:\n%s", out)
	}
	// Off: nothing tinted, no badge.
	m.blameRecent.on = false
	off := b.render(m, "")
	if strings.Contains(off, "48;2;") || strings.Contains(off, "≤7d") {
		t.Errorf("with the highlight off nothing may be tinted or badged:\n%q", off)
	}
}

// The Terminal theme has no tint role: the recent row goes bold instead, and
// the tint never appears.
func TestBlameRecentPaintsBoldOnTerminal(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Terminal)

	now := time.Now()
	m := Model{width: 100, height: 30, blameRecent: blameRecent{on: true, span: 7 * 24 * time.Hour}}
	out := recentFixture(now).render(m, "")
	fresh := lineWith(out, "fresh line")
	if fresh == "" {
		t.Fatalf("the recent line is missing:\n%s", out)
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
// "≤7d · /q  n/m".
func TestBlameRecentBadgeSharesSlotWithSearch(t *testing.T) {
	t.Parallel()
	m, b := blameSearchModel()
	m.blameRecent = blameRecent{on: true, span: 7 * 24 * time.Hour}
	m = typeBlame(m, b, "/", "m", "a", "i", "n", "enter")
	head := strings.SplitN(b.render(m, ""), "\n", 2)[0]
	if !strings.Contains(head, "≤7d · /main") {
		t.Errorf("header = %q, want the ≤7d · /main… badge", head)
	}
}
