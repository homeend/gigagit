package tui

import (
	"strings"
	"testing"
)

// Two files with remarks: in a review view's diff, } past the last remark
// of a.go steps to b.go (the review's own counts say it carries remarks),
// not "no next file with notes".
const twoFileReviewDoc = `{"version":1,"summary":"two","files":[
 {"path":"a.go","annotations":[{"newRange":[3,3],"summary":"on A"}]},
 {"path":"b.go","annotations":[{"newRange":[3,3],"summary":"on B"}]}]}`

func TestReviewDiffBraceStepsToTheNextRemarkedFile(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, twoFileReviewDoc)
	m, cmd := m.openReview(id, "Review")
	m = drainCmds(t, m, cmd)
	_, sel := filesLine(t, m, "a.go")
	m.filesView.sel = sel // enter on the row: the list's cursor sits on a.go
	m = openReviewDiff(t, m, "a.go")
	v := m.diffLayer()
	if v == nil {
		t.Fatal("no diff")
	}
	// First } lands on a.go's remark; the second arms the file step; the
	// third steps to b.go.
	for i := 0; i < 3; i++ {
		u, c := v.update(m, key("}"))
		m = drainCmds(t, u, c)
		v = m.diffLayer()
		if v == nil {
			t.Fatalf("} %d closed the diff", i+1)
		}
	}
	if strings.Contains(m.diffNotice, "no next file") || v.title != "b.go" {
		t.Fatalf("after three }: title %q notice %q", v.title, m.diffNotice)
	}
}

// Stacked, the same walk: } past a.go's remark continues into b.go (the
// stack's notedStackFile asks the review's counts too).
func TestStackedReviewDiffBraceStepsToTheNextRemarkedFile(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, twoFileReviewDoc)
	m, cmd := m.openReview(id, "Review")
	m = drainCmds(t, m, cmd)
	m = tempPromptStore(t, m)
	m = m.setStackedPref(true)
	m = openReviewDiff(t, m, "a.go")
	v := m.diffLayer()
	if v == nil || v.stk == nil {
		t.Fatal("the review diff must have opened stacked")
	}
	for i := 0; i < 2; i++ { // on a.go's remark, then into b.go
		u, c := v.update(m, key("}"))
		m = drainCmds(t, u, c)
		v = m.diffLayer()
	}
	if strings.Contains(m.diffNotice, "no next file") || v.stk.files[v.curFile()].path != "b.go" {
		t.Fatalf("after two }: file %q notice %q", v.stk.files[v.curFile()].path, m.diffNotice)
	}
}
