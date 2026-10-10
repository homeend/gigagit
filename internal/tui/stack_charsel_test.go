package tui

import "testing"

// openedReviewStack is the review view's stacked diff: the summary element
// first, then a.go; the model, the view and the first prose line's index.
func openedReviewStack(t *testing.T) (Model, *diffView, int) {
	t.Helper()
	m, id := reviewViewModel(t, charSelReviewDoc)
	m, cmd := m.openReview(id, "Review")
	m = drainCmds(t, m, cmd)
	m = m.setStackedPref(true)
	l, _ := filesLine(t, m, "a.go")
	u, cmd := m.openDiffForFileLine(l)
	m = drainCmds(t, u.(Model), cmd)
	v := m.diffLayer()
	if v == nil || v.stk == nil || !v.stk.files[0].summary {
		t.Fatalf("stack %+v, want the summary first", v)
	}
	for i, ln := range v.lines {
		if ln.kind == lineProse {
			return m, v, i
		}
	}
	t.Fatal("no prose line")
	return m, nil, -1
}

// v on a prose row selects within the summary element: the copy is the
// rendered row text (no "##"), and the element bounds the cursor.
func TestStackCharSelInTheSummaryElement(t *testing.T) {
	t.Parallel()
	m, v, first := openedReviewStack(t)
	v.setCursorLine(first, m.diffBodyRows())
	var got string
	m = captureClip(m, &got)
	m = feedDiff(m, "v")
	if !v.cs.on || v.csFile != 0 || v.cs.cur != (pos{first - v.csBase, 0}) {
		t.Fatalf("v: %+v base %d file %d", v.cs, v.csBase, v.csFile)
	}
	m = feedDiff(m, "k") // above the first prose row: the header, the rule — dead; the element's top
	if m.diffNotice != "▸ the selection stays in this element" {
		t.Fatalf("notice = %q", m.diffNotice)
	}
	m = feedDiff(m, "space", "end")
	u, cmd := m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if got != "Overview" {
		t.Fatalf("copied %q, want the rendered heading", got)
	}
}

// A wrapped paragraph in the stack's summary copies joined with a space.
func TestStackCharSelJoinsWrappedProse(t *testing.T) {
	t.Parallel()
	m, v, first := openedReviewStack(t)
	v.setCursorLine(first, m.diffBodyRows())
	var got string
	m = captureClip(m, &got)
	m = feedDiff(m, "v")
	rows := v.charRows()
	wrapAt := -1
	for i, r := range rows {
		if r.wraps {
			wrapAt = i
			break
		}
	}
	if wrapAt < 1 {
		t.Fatalf("no wrapped prose row among %d (widen charSelReviewDoc's paragraph)", len(rows))
	}
	for v.cs.cur.row < wrapAt-1 {
		m = feedDiff(m, "j")
	}
	m = feedDiff(m, "home", "space", "j", "l", "l")
	u, cmd := m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if want := string(rows[wrapAt-1].text) + " " + string(rows[wrapAt].text[:3]); got != want {
		t.Fatalf("copied %q\nwant   %q", got, want)
	}
}

// v on a file's row selects within that file on the cursor side; the file
// bounds the cursor (the notice names the file).
func TestStackCharSelInAFile(t *testing.T) {
	t.Parallel()
	m, v, _ := openedReviewStack(t)
	body := -1
	for i, ln := range v.lines {
		if ln.file == 1 && ln.isBody() && sidePresent(ln.Row, false) {
			body = i
			break
		}
	}
	if body < 0 {
		t.Fatal("a.go has no body line")
	}
	v.setCursorLine(body, m.diffBodyRows())
	var got string
	m = captureClip(m, &got)
	m = feedDiff(m, "v", "space", "l")
	if v.csFile != 1 {
		t.Fatalf("element = file %d, want a.go (1)", v.csFile)
	}
	m = feedDiff(m, "pgup") // the file's first live row at most; never into the summary
	if v.csFile != 1 || v.cs.cur.row < 0 || m.diffNotice != "▸ the selection stays in this file" {
		t.Fatalf("pgup left the file: file %d row %d notice %q", v.csFile, v.cs.cur.row, m.diffNotice)
	}
	m = feedDiff(m, "end") // the start is still column 0: the whole line
	u, cmd := m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if want := v.lines[body].Row.Right; got != want {
		t.Fatalf("copied %q, want %q", got, want)
	}
}

// Review Focus 4: v on the element's header lands on its first live row.
func TestStackCharSelFromAHeaderLandsOnTheFirstRow(t *testing.T) {
	t.Parallel()
	m, v, first := openedReviewStack(t)
	v.setCursorLine(v.stk.files[0].hdr, m.diffBodyRows())
	m = feedDiff(m, "v")
	if !v.cs.on || v.cs.cur.row != first-v.csBase {
		t.Fatalf("v on the header: %+v, want row %d", v.cs, first-v.csBase)
	}
}

// The stripe reaches the prose row's painter.
func TestStackCharSelPaintsProseRows(t *testing.T) {
	t.Parallel()
	m, v, first := openedReviewStack(t)
	v.setCursorLine(first, m.diffBodyRows())
	m = feedDiff(m, "v", "space", "l", "l")
	if mask := v.charEmphProse(first, 8); mask == nil || mask[0] != emphSel || mask[2] != emphSelCur {
		t.Fatalf("mask = %v", mask)
	}
}
