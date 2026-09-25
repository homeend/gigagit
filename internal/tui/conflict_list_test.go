package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// conflictListFixture is a list of long paths that share a long directory
// prefix and differ only in the filename — the case that used to render as a
// column of identical tail-cut rows.
func conflictListFixture(n int) []model.FileStatus {
	const dir = "core/llm/src/main/kotlin/com/intellij/ml/llm/matterhorn/tools/exceptions/"
	files := make([]model.FileStatus, n)
	for i := range files {
		files[i] = model.FileStatus{Path: fmt.Sprintf("%sRetryHandler%02d.kt", dir, i), Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'}
	}
	files[1].Staged, files[1].Unstaged = 'D', 'U' // "deleted by us, modified by them"
	return files
}

// Every row keeps its filename and its conflict label: the path is cut in the
// middle (elidePath), never at the end, and the label sits in its own column.
func TestConflictListRowsKeepFilenameAndLabel(t *testing.T) {
	t.Parallel()
	for _, width := range []int{80, 140} {
		m := Model{width: width, height: 40}
		files := conflictListFixture(5)
		out := ansi.Strip(conflictListBox(m, files, 2, domain.ConflictState{}, "", modeCutoff, 0))
		for _, f := range files {
			name := f.Path[strings.LastIndex(f.Path, "/")+1:]
			found := false
			for _, l := range strings.Split(out, "\n") {
				if strings.Contains(l, name) {
					found = true
					if !strings.Contains(l, conflictKindLabel(f)) {
						t.Errorf("width %d: row for %s lost its label %q:\n%s", width, name, conflictKindLabel(f), l)
					}
					if !strings.Contains(l, "core/") {
						t.Errorf("width %d: row for %s lost the path's beginning:\n%s", width, name, l)
					}
				}
			}
			if !found {
				t.Errorf("width %d: filename %s missing:\n%s", width, name, out)
			}
		}
	}
}

// The list box is the wide path-list popup (like the bookmark/shelf
// switchers), not the 56-column prose popup.
func TestConflictListBoxIsWide(t *testing.T) {
	t.Parallel()
	m := Model{width: 140, height: 40}
	out := conflictListBox(m, conflictListFixture(3), 0, domain.ConflictState{}, "", modeCutoff, 0)
	if got, want := lipgloss.Width(out), popupWideInnerWidth(140); got < want {
		t.Errorf("box width = %d, want >= %d", got, want)
	}
}

// The title counts the selection so a long, scrolled list says where you are.
func TestConflictListTitleCountsSelection(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40}
	out := ansi.Strip(conflictListBox(m, conflictListFixture(20), 6, domain.ConflictState{}, "", modeCutoff, 0))
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Resolve conflicts") {
			if !strings.Contains(l, "7/20") {
				t.Errorf("title line lacks 7/20:\n%s", l)
			}
			return
		}
	}
	t.Errorf("no title line:\n%s", out)
}
