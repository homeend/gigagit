package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notemeta"
)

const reviewMetaDoc = `{"version":1,"summary":"s","display":{"kind":{"show":["badge","count"],"values":{"issue":{"color":"red","audience":"author"}}}},"files":[
 {"path":"a.go","annotations":[{"newRange":[3,3],"summary":"A is unused","meta":{"kind":"issue"}}]},
 {"path":"zzz.go","annotations":[{"newRange":[1,1],"summary":"not in this commit","meta":{"kind":"issue"}}]}]}`

func TestReviewMetaLineCounts(t *testing.T) {
	t.Parallel()
	st := &reviewViewState{review: domain.Review{Agent: "Claude Code"},
		meta: notemeta.Counts{Groups: []notemeta.CountGroup{{Key: "kind", Values: []notemeta.CountValue{{Value: "issue", N: 3}, {Value: "good", N: 8}}}}, Author: 5, Reviewer: 9}}
	var m Model
	m.filesReview = st
	if got := m.filesCountsLine(); got != "3 issue · 8 good · "+i18n.T("%d for author", 5)+" · "+i18n.T("%d for you", 9) {
		t.Fatalf("counts line = %q", got)
	}
	st.meta = notemeta.Counts{}
	if got := m.filesCountsLine(); got != "" {
		t.Fatalf("no counts must add nothing: %q", got)
	}
}

func TestReviewFileRowCountsFitOrGo(t *testing.T) {
	t.Parallel()
	st := &reviewViewState{metaByFile: map[string]notemeta.Counts{"a.txt": {Groups: []notemeta.CountGroup{{Key: "kind", Values: []notemeta.CountValue{{Value: "issue", Color: "red", N: 1}}}}}}}
	tail, cls := reviewFileCountsTail(st, "a.txt", 40)
	if tail != "  issue 1" || len(cls) != len([]rune(tail)) || cls[len(cls)-1] != metaClass("red", notemeta.StyleText) {
		t.Fatalf("tail = %q %v", tail, cls)
	}
	if tail, _ := reviewFileCountsTail(st, "a.txt", 5); tail != "" {
		t.Fatalf("a tail that does not fit must go before the path is cut: %q", tail)
	}
	if tail, _ := reviewFileCountsTail(st, "b.txt", 40); tail != "" {
		t.Fatal("a file without counts has no tail")
	}
}

func TestReviewOtherNoteRowBadges(t *testing.T) {
	t.Parallel()
	st := &reviewViewState{other: []domain.ReviewOtherNote{{Path: "x.go", Range: [2]int{4, 4}, Summary: "gone",
		Meta: notemeta.Resolved{Badges: []notemeta.Badge{{Key: "kind", Value: "issue", Color: "red", Style: notemeta.StyleFilled}}}}}}
	ls := reviewOtherNoteLines(st)
	if !strings.HasPrefix(ls[0].text, " issue  x.go:4 — gone") || len(ls[0].cls) != len([]rune(ls[0].text)) || ls[0].cls[0] != metaClass("red", notemeta.StyleFilled) {
		t.Fatalf("row = %q %v", ls[0].text, ls[0].cls)
	}
	// The summary's "Other notes" rows indent by two: the mask moves with them.
	st.review = domain.Review{Doc: &notebatch.ReviewDoc{Summary: "s"}}
	for _, l := range reviewSummaryLines(st) {
		if strings.Contains(l.text, "x.go:4") && (len(l.cls) != len([]rune(l.text)) || l.cls[2] != metaClass("red", notemeta.StyleFilled)) {
			t.Fatalf("summary row = %q %v", l.text, l.cls)
		}
	}
}

func TestReviewViewShowsMetaCounts(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewMetaDoc)
	m, cmd := m.openReview(id, "Review")
	m = drainCmds(t, m, cmd)
	if m.filesReview == nil || m.filesReview.meta.Author != 2 {
		t.Fatalf("counts = %+v", m.filesReview)
	}
	// A file row counts only what its ◆N counts: zzz.go is not in the
	// commit, so its remark is an Other note, on no file's tail.
	if _, ok := m.filesReview.metaByFile["zzz.go"]; ok || m.filesReview.metaByFile["a.go"].Author != 1 {
		t.Fatalf("byFile = %+v", m.filesReview.metaByFile)
	}
	if !strings.Contains(m.filesCountsLine(), "2 issue") {
		t.Fatalf("counts = %q", m.filesCountsLine())
	}
	out := ansiStrip(m.renderFilesView(80, 20))
	row := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "a.go") {
			row = l
		}
	}
	if !strings.Contains(row, "issue 1") {
		t.Fatalf("file row = %q\n%s", row, out)
	}
	// Narrow: the counts go, the file name stays.
	out = ansiStrip(m.renderFilesView(18, 20))
	if strings.Contains(out, "issue 1") || !strings.Contains(out, "a.go") {
		t.Fatalf("narrow:\n%s", out)
	}
}

// The counts ride their own line under the review's meta line: at the
// sidebar's width they must be on screen, not cut off its end.
func TestReviewViewHeaderCountsVisible(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewMetaDoc)
	m, cmd := m.openReview(id, "Review")
	m = drainCmds(t, m, cmd)
	out := ansiStrip(m.renderFilesView(52, 20))
	if !strings.Contains(out, "2 issue · "+i18n.T("%d for author", 2)) {
		t.Fatalf("counts not visible:\n%s", out)
	}
	if strings.Contains(reviewMetaLine(m.filesReview), "2 issue") {
		t.Fatal("the counts moved to their own line")
	}
	if m.filesChromeLines() != 3 {
		t.Fatalf("chrome lines = %d, want title + meta + counts", m.filesChromeLines())
	}
}
