package model

import "testing"

func TestIsOverviewNote(t *testing.T) {
	t.Parallel()
	commit := FileAddress{State: StateCommitted, Commit: "abc"}
	ov := Note{Address: commit, Tags: []string{OverviewTag}}
	if !ov.IsOverviewNote() || ov.IsReviewNote() {
		t.Fatalf("overview note: IsOverviewNote=%v IsReviewNote=%v", ov.IsOverviewNote(), ov.IsReviewNote())
	}
	if (Note{Address: commit, Tags: []string{ReviewTag}}).IsOverviewNote() {
		t.Fatal("a review is not an overview")
	}
	withPath := Note{Address: FileAddress{State: StateCommitted, Commit: "abc", Path: "a.go"}, Tags: []string{OverviewTag}}
	if withPath.IsOverviewNote() {
		t.Fatal("a line note is never an overview, whatever its tags")
	}
}

func TestOverviewHintParses(t *testing.T) {
	t.Parallel()
	l, err := ParseLink("gg://github.com/o/r@0123456789abcdef0123456789abcdef01234567?overview=n12")
	if err != nil {
		t.Fatal(err)
	}
	if l.Hint.Kind != OverviewHintKind || l.Hint.ID != "n12" {
		t.Fatalf("hint = %+v", l.Hint)
	}
}
