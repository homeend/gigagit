package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// histModel is a real Model whose history holds `links`, recorded oldest
// first, with the `#` prompt open and its history load already landed.
func histModel(t *testing.T, links ...string) Model {
	t.Helper()
	m, _ := switcherLinkModel(t)
	for _, l := range links {
		m.svc.RecordCopiedLink(context.Background(), l)
	}
	m, cmd := m.openGotoCommitPopup()
	for _, msg := range runCmds(cmd) {
		nm, _ := m.Update(msg)
		m = nm.(Model)
	}
	return m
}

func gotoPopup(t *testing.T, m Model) *gotoCommitPopup {
	t.Helper()
	p, ok := m.topLayer().(*gotoCommitPopup)
	if !ok {
		t.Fatalf("top layer = %T, want the # prompt", m.topLayer())
	}
	return p
}

func typeKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func TestTheHashPromptLoadsTheHistoryNewestFirst(t *testing.T) {
	t.Parallel()
	m := histModel(t, "gg://r@ref:one", "gg://r@ref:two")
	p := gotoPopup(t, m)
	if !p.hist.loaded || len(p.hist.rows) != 2 || p.hist.rows[0].link != "gg://r@ref:two" || p.hist.rows[0].desc != "branch: two" {
		t.Fatalf("rows = %+v loaded=%v", p.hist.rows, p.hist.loaded)
	}
}

func TestAStaleHistoryLoadIsDropped(t *testing.T) {
	t.Parallel()
	m := histModel(t)
	stale := linkHistLoadedMsg{gen: m.linkHistGen - 1, rows: []histRow{{link: "gg://r@ref:stale"}}}
	nm, _ := m.Update(stale)
	if p := gotoPopup(t, nm.(Model)); len(p.hist.rows) != 0 {
		t.Fatalf("a superseded load landed: %+v", p.hist.rows)
	}
}

func TestPickingAHistoryRowSubmitsThatLink(t *testing.T) {
	t.Parallel()
	m := histModel(t, "gg://r@ref:one", "gg://r@ref:two", "gg://r@ref:three")
	for _, k := range []tea.KeyType{tea.KeyDown, tea.KeyDown} { // into the list, then to row 1
		nm, _ := m.Update(typeKey(k))
		m = nm.(Model)
	}
	nm, cmd := m.Update(typeKey(tea.KeyEnter))
	p := gotoPopup(t, nm.(Model))
	if got := p.input.Value(); got != "gg://r@ref:two" {
		t.Fatalf("field = %q, want the SECOND row's link", got)
	}
	// Submitted, through the link resolver: the popup is resolving and the cmd
	// answers with a link-resolve message for exactly that text.
	if !p.resolving || cmd == nil {
		t.Fatalf("resolving=%v cmd=%v: a pick must submit", p.resolving, cmd)
	}
	msgs := runCmds(cmd)
	if lm, ok := msgs[0].(gotoLinkResolvedMsg); !ok || lm.text != "gg://r@ref:two" {
		t.Fatalf("msg = %#v", msgs[0])
	}
}

func TestEscInTheListReturnsToTheFieldAndASecondEscCloses(t *testing.T) {
	t.Parallel()
	m := histModel(t, "gg://r@ref:one")
	nm, _ := m.Update(typeKey(tea.KeyDown))
	m = nm.(Model)
	if !gotoPopup(t, m).hist.active {
		t.Fatal("↓ did not enter the list")
	}
	nm, _ = m.Update(typeKey(tea.KeyEsc))
	m = nm.(Model)
	if p := gotoPopup(t, m); p.hist.active { // gotoPopup fails the test if the popup closed
		t.Fatal("esc left the list active")
	}
	nm, _ = m.Update(typeKey(tea.KeyEsc))
	if _, still := nm.(Model).topLayer().(*gotoCommitPopup); still {
		t.Fatal("the second esc must close the prompt")
	}
}

func TestTypingInTheListGoesBackToTheField(t *testing.T) {
	t.Parallel()
	m := histModel(t, "gg://r@ref:one")
	nm, _ := m.Update(typeKey(tea.KeyDown))
	nm, _ = nm.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	p := gotoPopup(t, nm.(Model))
	if p.hist.active || p.input.Value() != "a" {
		t.Fatalf("active=%v field=%q: the rune must land in the field", p.hist.active, p.input.Value())
	}
}

func TestUpOnTheFirstRowReturnsToTheField(t *testing.T) {
	t.Parallel()
	m := histModel(t, "gg://r@ref:one", "gg://r@ref:two")
	nm, _ := m.Update(typeKey(tea.KeyDown))
	nm, _ = nm.(Model).Update(typeKey(tea.KeyUp))
	if gotoPopup(t, nm.(Model)).hist.active {
		t.Fatal("↑ on row 0 must hand the keyboard back")
	}
}

func TestAnEmptyHistorySaysSoAndDownIsInert(t *testing.T) {
	t.Parallel()
	m := histModel(t)
	p := gotoPopup(t, m)
	if !strings.Contains(p.box(m), "no copied links yet") {
		t.Fatalf("box:\n%s", p.box(m))
	}
	nm, _ := m.Update(typeKey(tea.KeyDown))
	if gotoPopup(t, nm.(Model)).hist.active {
		t.Fatal("↓ entered an empty list")
	}
}

// The store caps at 20 on its own, so a list longer than that can only be
// built by hand — which is the point: the clamp is the PICKER's, and must
// hold whatever the store decides later.
func TestThePickerShowsAtMostTwentyRows(t *testing.T) {
	t.Parallel()
	m := histModel(t)
	rows := make([]histRow, 25)
	for i := range rows {
		rows[i] = histRow{link: "gg://r@ref:b", desc: "branch: b"}
	}
	nm, _ := m.Update(linkHistLoadedMsg{gen: m.linkHistGen, rows: rows})
	p := gotoPopup(t, nm.(Model))
	if len(p.hist.rows) != linkHistMax || strings.Count(p.hist.view(80), "\n") != linkHistMax-1 {
		t.Fatalf("rows=%d lines=%d", len(p.hist.rows), strings.Count(p.hist.view(80), "\n")+1)
	}
}

// No row may exceed the width it was given — with WIDE glyphs in the
// description, the class that once overflowed every list row in tmux.
func TestPickerRowsNeverExceedTheirWidth(t *testing.T) {
	t.Parallel()
	p := linkHistPicker{loaded: true, active: true, rows: []histRow{
		{desc: "bookmark: ☰ 日本語のラベル 😀😀😀😀😀😀😀😀😀😀", link: "gg://some-repository/internal/very/long/path/to/a/file.go@" + strings.Repeat("a", 40) + "?bookmark=abc"},
		{desc: "branch: x", link: "gg://r@ref:x"},
	}}
	for _, width := range []int{20, 40, 61, 120} {
		for i, line := range strings.Split(p.view(width), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d row %d is %d columns: %q", width, i, w, line)
			}
		}
	}
	// The elided link keeps BOTH ends: the repo and the hint tell two apart.
	got := elideMiddle("gg://some-repository/a/b/c.go@"+strings.Repeat("a", 40)+"?bookmark=abc", 30)
	if !strings.HasPrefix(got, "gg://some") || !strings.HasSuffix(got, "mark=abc") || lipgloss.Width(got) > 30 {
		t.Errorf("elideMiddle = %q", got)
	}
}
