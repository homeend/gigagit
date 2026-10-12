package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestPreviewOverviewLines(t *testing.T) {
	t.Parallel()
	files := []contentLine{{text: "a.go", path: "a.go"}}
	heads := []domain.OverviewHead{{ID: "o1", Title: "How #7 works", Agent: "Claude Code", Created: time.Now()},
		{ID: "o2", Title: "Old one", Agent: "Claude Code", Older: true, Created: time.Now()}}
	got := previewOverviewLines(heads, files)
	if len(got) != 4 || !got[0].heading || got[0].text != "Overviews" || got[1].overviewID != "o1" || got[3].path != "a.go" {
		t.Fatalf("lines = %+v", got)
	}
	if !strings.Contains(got[2].text, "older tip") {
		t.Fatalf("an older overview says so: %q", got[2].text)
	}
	// idempotent: re-laying replaces the block, never stacks a second one
	if again := previewOverviewLines(heads, got); len(again) != 4 {
		t.Fatalf("re-laid = %d lines", len(again))
	}
}

// Reviews on top, then Overviews, then the files — and re-laying either
// block (a counts reload, fresh reviews) never stacks a second copy.
func TestPreviewBlocksRelayout(t *testing.T) {
	t.Parallel()
	files := []contentLine{{text: "a.go", path: "a.go"}}
	revs := []domain.ReviewHead{{ID: "r1", Summary: "a review"}}
	ovs := []domain.OverviewHead{{ID: "o1", Title: "How #7 works"}}
	got := layPreviewBlocks(revs, ovs, files)
	again := layPreviewBlocks(revs, ovs, got)
	for _, ls := range [][]contentLine{got, again} {
		if len(ls) != 5 || ls[0].text != "Reviews" || ls[1].noteID != "r1" || ls[2].text != "Overviews" || ls[3].overviewID != "o1" || ls[4].path != "a.go" {
			t.Fatalf("lines = %+v", ls)
		}
	}
	if gone := layPreviewBlocks(revs, nil, again); len(gone) != 3 || gone[2].path != "a.go" {
		t.Fatalf("overviews gone = %+v", gone)
	}
}

// A PR view lists the PR's overviews from the loaded counts: its . menu is
// Open + Delete, and a counts reload without it drops the block.
func TestPRViewOverviewsFollowTheCounts(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.go", path: "a.go"}}}
	m.filesTreeFocused, m.filesMode = true, filesModeCompare
	m.filesPreviewSet = &domain.PreviewNoteSet{Source: "refs/gg/pr/7", Target: "main", Tip: "abc"}
	m.noteCounts.PreviewOverviews = map[string][]domain.OverviewHead{
		"main...refs/gg/pr/7": {{ID: "o1", Commit: "abc", Title: "How #7 works", Agent: "Claude Code"}}}
	m = m.setPreviewReviews(nil)
	if ls := m.filesView.lines; len(ls) != 3 || ls[1].overviewID != "o1" {
		t.Fatalf("lines = %+v", ls)
	}
	m.filesView.sel = 1
	rows, ok := m.noteRowMenu()
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.id)
	}
	if !ok || strings.Join(ids, "|") != "open-overview|delete-overview" {
		t.Fatalf("menu = %v", ids)
	}
	m, _ = m.reloadSourcesCmd([]sourceKey{srcNotes}, reloadOpts{})
	u, _ := m.Update(dataAvailableMsg{source: srcNotes, gen: m.srcGen[srcNotes], value: domain.NoteCounts{}})
	m = u.(Model)
	if ls := m.filesView.lines; len(ls) != 1 || ls[0].path != "a.go" {
		t.Fatalf("after the reload = %+v", ls)
	}
}
