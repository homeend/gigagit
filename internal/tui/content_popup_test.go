package tui

import (
	"fmt"
	"testing"
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
