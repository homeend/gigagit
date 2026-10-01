package tui

import (
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A note at this commit on a path the commit does not change, written
// outside any range, is listed; a changed file's
// note badges its own row instead, and another commit's notes never count.
func TestNotedElsewhere(t *testing.T) {
	t.Parallel()
	counts := map[string]int{"abc:a.txt": 1, "abc:c.txt": 1, "abc:z/y.txt": 2, "def:b.txt": 1}
	files := []model.CommitFile{{Path: "c.txt", Status: "A"}}
	if got, want := notedElsewhere(counts, "abc", files), []string{"a.txt", "z/y.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("notedElsewhere = %q, want %q", got, want)
	}
	if got := notedElsewhere(counts, "def", []model.CommitFile{{Path: "b.txt"}}); got != nil {
		t.Fatalf("a changed file's note listed again: %q", got)
	}
}

// The Notes group reads before the files; its rows are not files (no path),
// so the stack, N/P and every file action pass them by.
func TestWithNotedLines(t *testing.T) {
	t.Parallel()
	lines := withNotedLines([]string{"a.txt"}, commitFileLines([]model.CommitFile{{Path: "c.txt", Status: "A"}}))
	if len(lines) != 3 || !lines[0].heading || lines[1].notedPath != "a.txt" || lines[1].path != "" || lines[2].path != "c.txt" {
		t.Fatalf("lines = %+v", lines)
	}
	if got := withNotedLines([]string{"a.txt"}, commitFileLines(nil)); len(got) != 2 {
		t.Fatalf("(no files) must give way to the notes: %+v", got)
	}
	if got := withNotedLines(nil, commitFileLines(nil)); len(got) != 1 {
		t.Fatalf("no notes: the list is unchanged: %+v", got)
	}
}

// A scope reads the Previews panel's way round; a pair keeps its a..b.
func TestScopeLabel(t *testing.T) {
	t.Parallel()
	if got := scopeLabel("main...feature"); got != "feature → main" {
		t.Fatalf("merge preview = %q", got)
	}
	if got := scopeLabel("aaaaaaa..bbbbbbb"); got != "aaaaaaa..bbbbbbb" {
		t.Fatalf("pair = %q", got)
	}
}
