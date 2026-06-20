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
	m = m.pushLayer(newContentPopup("Test content", contentLines(n)))
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

func TestContentPopupZCyclesMode(t *testing.T) {
	m := contentModel(4)
	if layerOf[*contentPopup](m).mode != modeCutoff {
		t.Fatalf("default mode = %v, want modeCutoff", layerOf[*contentPopup](m).mode)
	}
	u, _ := m.Update(keyMsg("z"))
	mm := u.(Model)
	if layerOf[*contentPopup](mm).mode != modeWrap {
		t.Fatalf("after z, mode = %v, want modeWrap", layerOf[*contentPopup](mm).mode)
	}
}

// In the default cutoff mode the popup renders its rows just as before the
// renderWindow conversion: the cursor row prefixed, all rows present.
func TestContentPopupCutoffRendersRows(t *testing.T) {
	m := contentModel(4)
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "> line-00") {
		t.Fatalf("cursor row missing its prefix:\n%s", out)
	}
	for i := 0; i < 4; i++ {
		if !strings.Contains(out, fmt.Sprintf("line-%02d", i)) {
			t.Fatalf("line-%02d missing:\n%s", i, out)
		}
	}
}

// z must NOT cycle the mode while the /-search input is capturing keys — it
// types a literal "z" into the query instead.
func TestContentPopupZTypesWhileSearching(t *testing.T) {
	m := contentModel(4)
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("z"))
	mm := u.(Model)
	if layerOf[*contentPopup](mm).mode != modeCutoff {
		t.Fatalf("z while typing must not change mode; got %v", layerOf[*contentPopup](mm).mode)
	}
	if layerOf[*contentPopup](mm).query != "z" {
		t.Fatalf("z while typing must append to query; query = %q", layerOf[*contentPopup](mm).query)
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
	p := layerOf[*contentPopup](m)
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
	u, _ := m.Update(keyMsg("/")) // search starts only on an explicit /
	m = u.(Model)
	for _, r := range "line-2" { // matches line-20 … line-29 only
		u, _ = m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	p := layerOf[*contentPopup](m)
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
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	for _, r := range "zzz" {
		u, _ = m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "(no match)") {
		t.Fatalf("want (no match) marker:\n%s", out)
	}
}

func TestContentPopupEscStages(t *testing.T) {
	m := contentModel(5)
	for _, k := range []string{"/", "x", "enter"} { // search "x", commit it
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	if layerOf[*contentPopup](m) == nil || layerOf[*contentPopup](m).query != "x" || layerOf[*contentPopup](m).typing {
		t.Fatalf("enter must commit the search, got %+v", layerOf[*contentPopup](m))
	}
	u, _ := m.Update(keyMsg("esc")) // first esc clears the committed search
	m = u.(Model)
	if layerOf[*contentPopup](m) == nil {
		t.Fatal("first esc must only clear the search")
	}
	if layerOf[*contentPopup](m).query != "" {
		t.Fatalf("query = %q, want empty", layerOf[*contentPopup](m).query)
	}
	u, _ = m.Update(keyMsg("esc")) // second esc closes
	m = u.(Model)
	if layerOf[*contentPopup](m) != nil {
		t.Fatal("second esc must close the popup")
	}
}

func TestContentPopupEscCancelsSearchInput(t *testing.T) {
	m := contentModel(5)
	for _, k := range []string{"/", "x", "esc"} { // esc mid-input cancels
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	if layerOf[*contentPopup](m) == nil {
		t.Fatal("esc during search input must not close the popup")
	}
	if layerOf[*contentPopup](m).typing || layerOf[*contentPopup](m).query != "" {
		t.Fatalf("esc must cancel input and clear the query, got %+v", layerOf[*contentPopup](m))
	}
}

func TestContentPopupQCloses(t *testing.T) {
	m := contentModel(5)
	u, cmd := m.Update(keyMsg("q"))
	m = u.(Model)
	if layerOf[*contentPopup](m) != nil {
		t.Fatal("q must close the popup")
	}
	if cmd != nil {
		t.Fatal("q must close the window, not quit the app")
	}
}

func TestContentPopupVimKeysScroll(t *testing.T) {
	m := contentModel(30)
	p := layerOf[*contentPopup](m)
	u, _ := m.Update(keyMsg("j"))
	m = u.(Model)
	if p.sel != 1 {
		t.Errorf("j: sel = %d, want 1", p.sel)
	}
	u, _ = m.Update(keyMsg("k"))
	m = u.(Model)
	if p.sel != 0 {
		t.Errorf("k: sel = %d, want 0", p.sel)
	}
	u, _ = m.Update(keyMsg("/")) // in search mode j filters instead
	m = u.(Model)
	u, _ = m.Update(keyMsg("j"))
	m = u.(Model)
	if p.sel != 0 || p.query != "j" {
		t.Errorf("j while searching must edit the query, got sel=%d query=%q", p.sel, p.query)
	}
}

func TestContentPopupEnterCloses(t *testing.T) {
	m := contentModel(5)
	u, _ := m.Update(keyMsg("enter"))
	m = u.(Model)
	if layerOf[*contentPopup](m) != nil {
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
	if layerOf[*contentPopup](m) == nil || layerOf[*contentPopup](m).query != "" {
		t.Fatalf("p must be inert outside search mode, got %+v", layerOf[*contentPopup](m))
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
	p := layerOf[*contentPopup](m)
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
	if layerOf[*contentPopup](u.(Model)) != nil {
		t.Fatal("mouse must be inert when no content popup is open")
	}
}

// TestContentPopupRowsNeverWrap pins a rendering regression: rows were
// truncated to the box Width, but lipgloss wraps text at Width minus the
// horizontal padding, so every full-width row spilled a "…" fragment onto a
// continuation line. A long row must occupy exactly one rendered line.
func TestContentPopupRowsNeverWrap(t *testing.T) {
	m := Model{width: 80, height: 24}
	m = m.pushLayer(newContentPopup("T", []contentLine{
		{text: strings.Repeat("a", 200)},
	}))
	out := ansi.Strip(m.render())
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "aaa") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("a long row must render as exactly 1 line, got %d:\n%s", n, out)
	}
}

// TestContentPopupUsesWideBox: the key table needs more room than the
// standard 56-column form popup; on a wide terminal an 80-char row must
// survive untruncated.
func TestContentPopupUsesWideBox(t *testing.T) {
	for _, c := range []struct{ w, want int }{{160, 100}, {80, 72}, {30, 22}} {
		if got := contentPopupWidth(c.w); got != c.want {
			t.Errorf("contentPopupWidth(%d) = %d, want %d", c.w, got, c.want)
		}
	}
	m := Model{width: 120, height: 24}
	m = m.pushLayer(newContentPopup("T", []contentLine{
		{text: strings.Repeat("b", 80)},
	}))
	out := ansi.Strip(m.render())
	if !strings.Contains(out, strings.Repeat("b", 80)) {
		t.Fatalf("80-char row must fit untruncated at 120 cols:\n%s", out)
	}
}
