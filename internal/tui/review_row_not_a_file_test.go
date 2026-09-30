package tui

import (
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// reviewRowFilesModel is a commit's files view listing one AI review (the
// @notes/ row, which carries a virtual path) above two files.
func reviewRowFilesModel() Model {
	m := diffModel()
	m.width, m.height = 100, 40
	m.filesView = &contentPopup{lines: withReviewLines(
		[]domain.Review{{ID: "rev1", Agent: "Claude Code", Created: time.Now()}},
		[]contentLine{{text: "a.go", path: "a.go", status: "M"}, {text: "b.go", path: "b.go", status: "M"}})}
	m.filesMode = filesModeChanged
	m.filesHash = "c0ffeeaa"
	return m
}

// The review is not a file: a stack opened from a file row holds the files
// only, never the review's raw text as an all-added "file".
func TestStackSkipsTheReviewRow(t *testing.T) {
	t.Parallel()
	m := reviewRowFilesModel()
	fs := m.buildTreeStack()
	if len(fs) != 2 || fs[0].path != "a.go" || fs[1].path != "b.go" {
		var got []string
		for _, f := range fs {
			got = append(got, f.path)
		}
		t.Fatalf("stack = %q, want [a.go b.go]", got)
	}
}

// Stepping files in a diff (N/P) never lands on the review row either.
func TestFileStepSkipsTheReviewRow(t *testing.T) {
	t.Parallel()
	m := reviewRowFilesModel()
	m.diffNav = diffNavTree
	for i, l := range m.filesView.visible() {
		if l.path == "a.go" {
			m.filesView.sel = i
		}
	}
	if seq := m.diffFileSequence(-1); len(seq) != 0 {
		t.Fatalf("stepping back from the first file = %q, want nothing (the review is not a file)", seq)
	}
	if i := nextFileRow(m.filesView.visible(), m.filesView.sel, -1); i != -1 {
		t.Fatalf("P from the first file lands on row %d, want none (the review is not a file)", i)
	}
}
