package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// contentLines builds n filterable lines named line-00 … line-NN.
func contentLines(n int) []contentLine {
	out := make([]contentLine, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, contentLine{text: fmt.Sprintf("line-%02d detail", i)})
	}
	return out
}

func TestContentVisibleNoQueryReturnsAll(t *testing.T) {
	p := newContentPopup("T", contentLines(4))
	if got := len(p.visible()); got != 4 {
		t.Fatalf("visible() = %d lines, want 4", got)
	}
}

func TestContentVisibleFiltersAndKeepsMatchingHeadings(t *testing.T) {
	p := newContentPopup("T", []contentLine{
		{text: "Alpha", heading: true},
		{text: "apple one"},
		{text: "Beta", heading: true},
		{text: "berry two"},
	})
	p.query = "BERRY" // case-insensitive
	vis := p.visible()
	if len(vis) != 2 || !vis[0].heading || vis[0].text != "Beta" || vis[1].text != "berry two" {
		t.Fatalf("visible() = %+v, want [Beta(heading), berry two]", vis)
	}
}

func TestContentVisibleHeadingsAreNotMatchTargets(t *testing.T) {
	p := newContentPopup("T", []contentLine{
		{text: "Alpha", heading: true},
		{text: "apple one"},
	})
	p.query = "alpha" // matches only the heading text → nothing survives
	if vis := p.visible(); len(vis) != 0 {
		t.Fatalf("visible() = %+v, want empty (headings are never matched)", vis)
	}
}

func TestContentMoveClamps(t *testing.T) {
	p := newContentPopup("T", contentLines(10))
	p.move(-5)
	if p.sel != 0 {
		t.Errorf("move(-5) from 0: sel = %d, want 0", p.sel)
	}
	p.move(100)
	if p.sel != 9 {
		t.Errorf("move(100): sel = %d, want 9", p.sel)
	}
	p.move(-3)
	if p.sel != 6 {
		t.Errorf("move(-3) from 9: sel = %d, want 6", p.sel)
	}
}

func TestContentMoveOnEmptyVisible(t *testing.T) {
	p := newContentPopup("T", contentLines(3))
	p.query = "zzz-no-match"
	p.move(1)
	if p.sel != 0 {
		t.Errorf("move on empty visible: sel = %d, want 0", p.sel)
	}
}

// contentModel is an 80×24 model with an open content popup of n lines.
// At 24 rows the popup shows contentPageRows() = 24-7 = 17 rows, so n=5
// fits and n=30 overflows.
func contentModel(n int) Model {
	m := Model{width: 80, height: 24}
	m.contentPopup = newContentPopup("Test content", contentLines(n))
	return m
}

func TestContentPageRows(t *testing.T) {
	m := Model{width: 80, height: 24}
	if got := m.contentPageRows(); got != 17 {
		t.Errorf("contentPageRows at h=24 = %d, want 17", got)
	}
	m.height = 8
	if got := m.contentPageRows(); got != 3 {
		t.Errorf("contentPageRows at h=8 = %d, want floor 3", got)
	}
}

func TestContentPopupFitsViewport(t *testing.T) {
	m := contentModel(5)
	for i := 0; i < 4; i++ { // cursor to the last line — still no scrolling
		u, _ := m.Update(keyMsg("down"))
		m = u.(Model)
	}
	out := ansi.Strip(m.render())
	for i := 0; i < 5; i++ {
		if !strings.Contains(out, fmt.Sprintf("line-%02d", i)) {
			t.Fatalf("fitting content must be fully visible; line-%02d missing:\n%s", i, out)
		}
	}
	if strings.Contains(out, "5/5") {
		t.Error("no position indicator when the content fits")
	}
}

func TestContentPopupOverflowScrolls(t *testing.T) {
	m := contentModel(30)
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "line-00") || strings.Contains(out, "line-29") {
		t.Fatalf("initial window must show the top, not the bottom:\n%s", out)
	}
	if !strings.Contains(out, "1/30") {
		t.Errorf("overflowing content must show a position indicator:\n%s", out)
	}
	u, _ := m.Update(keyMsg("pgdown")) // +17
	m = u.(Model)
	u, _ = m.Update(keyMsg("pgdown")) // clamps to 29
	m = u.(Model)
	out = ansi.Strip(m.render())
	if !strings.Contains(out, "line-29") || strings.Contains(out, "line-00") {
		t.Fatalf("after paging to the end the window must show the bottom:\n%s", out)
	}
	if !strings.Contains(out, "30/30") {
		t.Errorf("indicator must track the cursor:\n%s", out)
	}
}

func TestContentPopupStepSizes(t *testing.T) {
	m := contentModel(30)
	p := m.contentPopup
	u, _ := m.Update(keyMsg("ctrl+down"))
	m = u.(Model)
	if p.sel != 5 {
		t.Errorf("ctrl+down: sel = %d, want 5", p.sel)
	}
	u, _ = m.Update(keyMsg("pgdown"))
	m = u.(Model)
	if p.sel != 22 {
		t.Errorf("pgdown: sel = %d, want 22 (5+17)", p.sel)
	}
	u, _ = m.Update(keyMsg("ctrl+up"))
	m = u.(Model)
	if p.sel != 17 {
		t.Errorf("ctrl+up: sel = %d, want 17", p.sel)
	}
	u, _ = m.Update(keyMsg("pgup"))
	m = u.(Model)
	if p.sel != 0 {
		t.Errorf("pgup: sel = %d, want 0", p.sel)
	}
}

func TestContentPopupSearchWhileScrolled(t *testing.T) {
	m := contentModel(30)
	for i := 0; i < 25; i++ {
		u, _ := m.Update(keyMsg("down"))
		m = u.(Model)
	}
	for _, r := range "line-2" { // matches line-20 … line-29 only
		u, _ := m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	p := m.contentPopup
	if p.sel != 0 {
		t.Errorf("query change must reset the cursor: sel = %d, want 0", p.sel)
	}
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "> line-20") {
		t.Fatalf("after searching while scrolled, the view must start at the first match:\n%s", out)
	}
}

func TestContentPopupNoMatch(t *testing.T) {
	m := contentModel(5)
	for _, r := range "zzz" {
		u, _ := m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "(no match)") {
		t.Fatalf("want (no match) marker:\n%s", out)
	}
}

func TestContentPopupEscTwoStage(t *testing.T) {
	m := contentModel(5)
	u, _ := m.Update(keyMsg("x"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("esc")) // first esc clears the query
	m = u.(Model)
	if m.contentPopup == nil {
		t.Fatal("first esc must only clear the query")
	}
	if m.contentPopup.query != "" {
		t.Fatalf("query = %q, want empty", m.contentPopup.query)
	}
	u, _ = m.Update(keyMsg("esc")) // second esc closes
	m = u.(Model)
	if m.contentPopup != nil {
		t.Fatal("second esc must close the popup")
	}
}

func TestContentPopupEnterCloses(t *testing.T) {
	m := contentModel(5)
	u, _ := m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.contentPopup != nil {
		t.Fatal("enter must close the read-only popup")
	}
}

func TestContentPopupSwallowsGlobalKeys(t *testing.T) {
	m := contentModel(5)
	u, _ := m.Update(keyMsg("p")) // global: SmartPull — must NOT fire
	m = u.(Model)
	if m.running {
		t.Fatal("p must not start an operation while the popup is open")
	}
	if m.contentPopup == nil || m.contentPopup.query != "p" {
		t.Fatalf("typed rune must go to the filter query, got %+v", m.contentPopup)
	}
}

func TestContentPopupFitBounds(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{80, 24}, {40, 10}, {30, 8}} {
		m := contentModel(40)
		m.width, m.height = sz.w, sz.h
		out := m.render()
		lines := strings.Split(out, "\n")
		if len(lines) > sz.h {
			t.Errorf("%dx%d: %d lines rendered, must be <= %d", sz.w, sz.h, len(lines), sz.h)
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > sz.w {
				t.Errorf("%dx%d: line %d is %d cols, must be <= %d", sz.w, sz.h, i, w, sz.w)
			}
		}
	}
}

// TestContentVisibleAdjacentHeadings locks in the pending-overwrite behavior:
// a heading with no matching rows before the next heading is dropped.
func TestContentVisibleAdjacentHeadings(t *testing.T) {
	p := newContentPopup("T", []contentLine{
		{text: "Alpha", heading: true},
		{text: "Beta", heading: true},
		{text: "apple row"},
	})
	p.query = "apple"
	vis := p.visible()
	if len(vis) != 2 || vis[0].text != "Beta" || vis[1].text != "apple row" {
		t.Fatalf("visible() = %+v, want [Beta(heading), apple row] — Alpha must be dropped", vis)
	}
}

func wheelMsg(up bool) tea.MouseMsg {
	b := tea.MouseButtonWheelDown
	if up {
		b = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{Button: b, Action: tea.MouseActionPress}
}

func TestContentPopupWheelScrolls(t *testing.T) {
	m := contentModel(30)
	p := m.contentPopup
	u, _ := m.Update(wheelMsg(false))
	m = u.(Model)
	if p.sel != 3 {
		t.Errorf("wheel down: sel = %d, want 3", p.sel)
	}
	u, _ = m.Update(wheelMsg(true))
	m = u.(Model)
	if p.sel != 0 {
		t.Errorf("wheel up: sel = %d, want 0", p.sel)
	}
}

func TestMouseIgnoredWithoutContentPopup(t *testing.T) {
	m := Model{width: 80, height: 24}
	u, _ := m.Update(wheelMsg(false)) // must not panic or change state
	if u.(Model).contentPopup != nil {
		t.Fatal("mouse must be inert when no content popup is open")
	}
}
