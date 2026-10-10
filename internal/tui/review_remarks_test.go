package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
)

// The file an overview of a review steered to draws the review's remark on
// it as a box (spec 2026-10-10-overview-split-all §6).
func TestSplitFilePaneDrawsTheReviewRemark(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t) // a.go: remark "A" on line 3
	r := sv.right.doc()
	ns := r.shownNotes()
	if len(ns) != 1 || ns[0].Summary != "A" || ns[0].Start != 3 {
		t.Fatalf("shown %+v", ns)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "│ A") && !strings.Contains(v, " A ") {
		t.Fatalf("no box for the remark:\n%s", v)
	}
}

// A remark is the review's: d and r on it say so and change nothing.
func TestReviewRemarkIsReadOnly(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	m = pressDrain(t, m, "tab")
	r := sv.right.doc()
	r.p.cur = 2 // line 3, under the remark
	for _, k := range []string{"d", "r"} {
		m = pressDrain(t, m, k)
		if len(r.shownNotes()) != 1 || !strings.Contains(m.statusMsg, "read-only") {
			t.Fatalf("%s: notes %d status %q", k, len(r.shownNotes()), m.statusMsg)
		}
	}
}

// } reaches the remark like any note.
func TestNextNoteReachesTheRemark(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	m = pressDrain(t, m, "tab")
	r := sv.right.doc()
	r.p.cur = 0
	pressDrain(t, m, "}")
	if r.p.cur != 2 {
		t.Fatalf("cur %d, want the remark's line 3", r.p.cur)
	}
}

// The remarks show only while the review's overview is the file's way back.
func TestRemarksLeaveWithTheOverview(t *testing.T) {
	t.Parallel()
	_, sv := reviewSplitModel(t)
	r := sv.right.doc()
	r.from = nil
	if len(r.shownNotes()) != 0 {
		t.Fatal("a review's remarks show only while its overview is the file's way back")
	}
}

// An answer for another review (the file was steered elsewhere meanwhile)
// is dropped.
func TestStaleRemarksAnswerIsDropped(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	r := sv.right.doc()
	r.reviewNotes, r.reviewNotesOf = nil, ""
	m.Update(reviewNotesMsg{slotStamp: m.stamp(), tag: r.tag, review: "some-other-review", notes: []domain.ResolvedNote{{Range: [2]int{1, 1}}}})
	if len(r.reviewNotes) != 0 {
		t.Fatal("an answer for another review is dropped")
	}
}

// A remark's box names the review, not its internal id.
func TestRemarkBoxTitleHidesTheInternalID(t *testing.T) {
	t.Parallel()
	n := remarkBoxes([]domain.ResolvedNote{{Range: [2]int{2, 2}}})[0]
	n.ID, n.Author = "review:32fff194:0", "Claude"
	if got := noteTitle(n); strings.Contains(got, "32fff194") || !strings.Contains(got, "review") {
		t.Fatalf("title %q", got)
	}
}
