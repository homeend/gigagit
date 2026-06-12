package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestCommitFileLinesGroupsByDirectory(t *testing.T) {
	files := []model.CommitFile{
		{Status: "M", Path: "internal/tui/model.go"},
		{Status: "A", Path: "internal/engine/smart_merge.go"},
		{Status: "M", Path: "CHANGELOG.md"},
		{Status: "A", Path: "internal/tui/mark.go"},
	}
	got := commitFileLines(files)
	want := []contentLine{
		{text: "M  CHANGELOG.md"},
		{text: "internal/engine/", heading: true},
		{text: "  A  smart_merge.go"},
		{text: "internal/tui/", heading: true},
		{text: "  A  mark.go"},
		{text: "  M  model.go"},
	}
	if len(got) != len(want) {
		t.Fatalf("lines = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCommitFileLinesEmitsEachDirHeadingOnce(t *testing.T) {
	// Path-sorted these interleave dir "a" with its subdirs (a/b/f < a/c.go
	// < a/d/g < a/e.go); the dir-major sort must emit heading "a/" once.
	files := []model.CommitFile{
		{Status: "M", Path: "a/c.go"},
		{Status: "M", Path: "a/b/f.go"},
		{Status: "M", Path: "a/e.go"},
		{Status: "M", Path: "a/d/g.go"},
	}
	got := commitFileLines(files)
	count := 0
	for _, l := range got {
		if l.heading && l.text == "a/" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("heading \"a/\" emitted %d times, want 1: %+v", count, got)
	}
}

func TestCommitFileLinesRename(t *testing.T) {
	files := []model.CommitFile{{Status: "R", Path: "b/new.go", OldPath: "a/old.go"}}
	got := commitFileLines(files)
	want := []contentLine{
		{text: "b/", heading: true},
		{text: "  R  a/old.go → new.go"},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("lines = %+v, want %+v", got, want)
	}
}

func TestCommitFileLinesEmpty(t *testing.T) {
	got := commitFileLines(nil)
	if len(got) != 1 || got[0].heading || got[0].text != "(no files)" {
		t.Fatalf("lines = %+v, want one non-heading \"(no files)\"", got)
	}
}
