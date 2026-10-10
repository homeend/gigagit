package tui

import (
	"strings"
	"testing"
)

const charSelReviewDoc = `{"version":1,"summary":"## Overview\nLooks fine, see A.\n\n` +
	`This paragraph is long enough to wrap inside the popup's reading column several times over, ` +
	`because the character selection must be tested across a wrapped row and not only on short ones.",` +
	`"meta":{"verdict":"approve"},"files":[{"path":"a.go","annotations":[{"newRange":[1,1],"summary":"adds A"}]}]}`

// openedSummaryPopup is the review view with its Summary popup on top.
func openedSummaryPopup(t *testing.T) (Model, *reviewSummaryPopup) {
	t.Helper()
	m, id := reviewViewModel(t, charSelReviewDoc)
	m, cmd := m.openReview(id, "Review")
	m = drainCmds(t, m, cmd)
	m, _ = m.openReviewSummary()
	p, ok := m.topLayer().(*reviewSummaryPopup)
	if !ok {
		t.Fatalf("top layer %T, want the summary popup", m.topLayer())
	}
	return m, p
}

func feedKeys(m Model, keys ...string) Model {
	for _, k := range keys {
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	return m
}

// v / l l l / space / … / enter copies the rendered text, not the markdown:
// the heading row reads "Overview", never "## Overview".
func TestSummaryCharSelCopiesRenderedText(t *testing.T) {
	t.Parallel()
	m, p := openedSummaryPopup(t)
	var got string
	m = captureClip(m, &got)
	m = feedKeys(m, "v")
	if !p.cs.on || p.cs.cur != (pos{0, 0}) {
		t.Fatalf("v: %+v", p.cs)
	}
	m = feedKeys(m, "space", "l", "l", "l")
	u, cmd := m.Update(keyMsg("enter"))
	m = drainCmds(t, u.(Model), cmd)
	if got != "Over" || p.cs.on {
		t.Fatalf("copied %q (on=%v), want Over", got, p.cs.on)
	}
	if !strings.Contains(m.statusMsg, "Copied 4 characters") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

// A selection across a wrapped row copies the paragraph joined with one
// space; the popup's own keys (y, o, /) are inert while the mode is on.
func TestSummaryCharSelJoinsAWrappedRowAndIsModal(t *testing.T) {
	t.Parallel()
	m, p := openedSummaryPopup(t)
	var got string
	m = captureClip(m, &got)
	m = feedKeys(m, "v")
	rows := p.charRows()
	wrapAt := -1
	for i, r := range rows {
		if r.wraps {
			wrapAt = i
			break
		}
	}
	if wrapAt < 1 {
		t.Fatalf("no wrapped row among %d rows", len(rows))
	}
	for p.cs.cur.row < wrapAt-1 {
		m = feedKeys(m, "j")
	}
	m = feedKeys(m, "home", "space", "j", "l", "l")
	before := got
	u, cmd := m.Update(keyMsg("y")) // y in the mode COPIES the selection (not the markdown)
	m = drainCmds(t, u.(Model), cmd)
	if got == before || strings.Contains(got, "##") {
		t.Fatalf("y copied %q", got)
	}
	want := string(rows[wrapAt-1].text) + " " + string(rows[wrapAt].text[:3])
	if got != want {
		t.Fatalf("copied %q\nwant   %q", got, want)
	}
	m = feedKeys(m, "v", "o", "/")
	if !p.cs.on || p.typing || m.topLayer() != p {
		t.Fatalf("o and / must be inert in the mode: on=%v typing=%v top=%T", p.cs.on, p.typing, m.topLayer())
	}
}

// esc drops the start, then leaves; the popup stays open.
func TestSummaryCharSelEscStages(t *testing.T) {
	t.Parallel()
	m, p := openedSummaryPopup(t)
	m = feedKeys(m, "v", "space", "l", "esc")
	if !p.cs.on || p.cs.fixed {
		t.Fatalf("first esc: %+v", p.cs)
	}
	m = feedKeys(m, "esc")
	if p.cs.on || m.topLayer() != p {
		t.Fatalf("second esc leaves the mode, not the popup: on=%v top=%T", p.cs.on, m.topLayer())
	}
}

// Review Focus 3: a relayout (ctrl+t) while the mode is on leaves it.
func TestSummaryCharSelLeavesOnRelayout(t *testing.T) {
	t.Parallel()
	m, p := openedSummaryPopup(t)
	m = feedKeys(m, "v", "space", "l")
	p.maximized = !p.maximized // what ctrl+t does to the box width
	_ = m.View()
	if p.cs.on {
		t.Fatal("a width change must leave the mode (the rows re-wrapped)")
	}
}

// The painted popup shows the stripe on the covered runes: the rendered
// view carries the selection style's escape before "ver" and the hint names
// the stage.
func TestSummaryCharSelPaintsAndHints(t *testing.T) {
	t.Parallel()
	m, p := openedSummaryPopup(t)
	m = feedKeys(m, "v")
	view := m.View()
	if !strings.Contains(view, "[space] start") {
		t.Fatalf("hint missing:\n%s", view)
	}
	m = feedKeys(m, "space", "l", "l")
	if !strings.Contains(m.View(), "[enter y] copy 3 chars") {
		t.Fatalf("fixed hint missing:\n%s", m.View())
	}
	if mask := p.charEmphFor(0, len([]rune("  Overview"))); mask == nil || mask[2] != emphSel || mask[4] != emphSelCur {
		t.Fatalf("row 0 mask = %v", mask)
	}
}

// F2: a copy or a leave must not scroll the popup back to the top.
func TestSummaryCharSelKeepsThePagerPlace(t *testing.T) {
	t.Parallel()
	m, p := openedSummaryPopup(t)
	var got string
	m = captureClip(m, &got)
	m = feedKeys(m, "down", "down", "down")
	if p.sel != 3 {
		t.Fatalf("pager top = %d, want 3", p.sel)
	}
	m = feedKeys(m, "v", "space", "l")
	u, cmd := m.Update(keyMsg("enter"))
	m = drainCmds(t, u.(Model), cmd)
	if p.sel != 3 || p.cs.on {
		t.Fatalf("after the copy the pager top = %d (on=%v), want 3", p.sel, p.cs.on)
	}
	m = feedKeys(m, "v", "esc")
	if p.sel != 3 {
		t.Fatalf("after leaving the pager top = %d, want 3", p.sel)
	}
}

const charSelURLDoc = `{"version":1,"summary":"See https://example.com/a/very/long/path/that/no/reading/column/can/hold/on/one/line/without/breaking/it/somewhere/in/the/middle/of/the/url/x for details.","files":[]}`

// F5: a wrap that broke a long word (a URL) joins with nothing.
func TestSummaryCharSelHardWrapJoinsWithoutASpace(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, charSelURLDoc)
	m, cmd := m.openReview(id, "Review")
	m = drainCmds(t, m, cmd)
	m, _ = m.openReviewSummary()
	p := m.topLayer().(*reviewSummaryPopup)
	var got string
	m = captureClip(m, &got)
	m = feedKeys(m, "v")
	rows := p.charRows()
	hardAt := -1
	for i, r := range rows {
		if r.wraps && r.hard {
			hardAt = i
			break
		}
	}
	if hardAt < 1 {
		t.Fatalf("no hard-wrapped row among %d rows: %q", len(rows), charRowTexts(rows))
	}
	for p.cs.cur.row < hardAt-1 {
		m = feedKeys(m, "j")
	}
	m = feedKeys(m, "end", "space", "j", "home")
	u, cmd := m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	prev := rows[hardAt-1].text
	if want := string(prev[len(prev)-1]) + string(rows[hardAt].text[0]); got != want {
		t.Fatalf("copied %q, want %q (no space at a hard break)", got, want)
	}
}

func charRowTexts(rows []charRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = string(r.text)
	}
	return out
}
