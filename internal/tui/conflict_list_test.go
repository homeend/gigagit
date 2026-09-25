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
		out := ansi.Strip(conflictListBox(m, files, 2, domain.ConflictState{}, "", modeCutoff, 0, false))
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
	out := conflictListBox(m, conflictListFixture(3), 0, domain.ConflictState{}, "", modeCutoff, 0, false)
	if got, want := lipgloss.Width(out), popupWideInnerWidth(140); got < want {
		t.Errorf("box width = %d, want >= %d", got, want)
	}
}

// The title counts the selection so a long, scrolled list says where you are.
func TestConflictListTitleCountsSelection(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40}
	out := ansi.Strip(conflictListBox(m, conflictListFixture(20), 6, domain.ConflictState{}, "", modeCutoff, 0, false))
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

// Maximized, the box is as wide as the longest FULL row — every path shows
// whole — and no wider (not the near-fullscreen width other popups take).
func TestConflictListMaximizedFitsLongestPath(t *testing.T) {
	t.Parallel()
	m := Model{width: 220, height: 50}
	files := conflictListFixture(4)
	out := conflictListBox(m, files, 0, domain.ConflictState{}, "", modeCutoff, 0, true)
	plain := ansi.Strip(out)
	for _, f := range files {
		if !strings.Contains(plain, f.Path) {
			t.Errorf("maximized list cut %s:\n%s", f.Path, plain)
		}
	}
	if got, full := lipgloss.Width(out), popupFullInnerWidth(220)+2; got >= full {
		t.Errorf("maximized box width = %d, want narrower than the full-screen %d (fit to content)", got, full)
	}
}

// Maximized width never exceeds the terminal's popup limit; a path longer than
// that is still middle-elided, never end-cut.
func TestConflictListMaximizedCappedToTerminal(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 50}
	out := conflictListBox(m, conflictListFixture(3), 0, domain.ConflictState{}, "", modeCutoff, 0, true)
	if got, lim := lipgloss.Width(out), popupFullInnerWidth(100)+2; got > lim {
		t.Errorf("maximized box width = %d, want <= %d", got, lim)
	}
	if !strings.Contains(ansi.Strip(out), "RetryHandler02.kt") {
		t.Errorf("capped maximized list lost a filename:\n%s", ansi.Strip(out))
	}
}

// Short paths: maximizing never shrinks the box below its normal width.
func TestConflictListMaximizedNeverNarrower(t *testing.T) {
	t.Parallel()
	m := Model{width: 200, height: 50}
	files := []model.FileStatus{{Path: "a.go", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'}}
	normal := conflictListBox(m, files, 0, domain.ConflictState{}, "", modeCutoff, 0, false)
	maxed := conflictListBox(m, files, 0, domain.ConflictState{}, "", modeCutoff, 0, true)
	if lipgloss.Width(maxed) < lipgloss.Width(normal) {
		t.Errorf("maximized %d narrower than normal %d", lipgloss.Width(maxed), lipgloss.Width(normal))
	}
}

// Maximized, the list shows as many files as the terminal height allows
// (normal caps at 12).
func TestConflictListMaximizedShowsMoreRows(t *testing.T) {
	t.Parallel()
	m := Model{width: 200, height: 50}
	files := conflictListFixture(30)
	count := func(out string) int {
		return strings.Count(ansi.Strip(out), "RetryHandler")
	}
	if n := count(conflictListBox(m, files, 0, domain.ConflictState{}, "", modeCutoff, 0, false)); n != 12 {
		t.Errorf("normal rows = %d, want 12", n)
	}
	if n := count(conflictListBox(m, files, 0, domain.ConflictState{}, "", modeCutoff, 0, true)); n != 30 {
		t.Errorf("maximized rows = %d, want all 30", n)
	}
	if h := lipgloss.Height(conflictListBox(m, files, 0, domain.ConflictState{}, "", modeCutoff, 0, true)); h > 50 {
		t.Errorf("maximized box height %d exceeds the terminal", h)
	}
}

// ctrl+t toggles the list's maximize; esc restores a maximized list before a
// second esc leaves the process.
func TestConflictListCtrlTTogglesAndEscRestores(t *testing.T) {
	t.Parallel()
	p := &conflictProcess{st: confListing, files: conflictListFixture(2)}
	m := Model{width: 120, height: 40}
	m.proc = p
	m, _ = p.update(m, keyMsg("ctrl+t"))
	if !p.maxed() {
		t.Fatal("ctrl+t did not maximize the list")
	}
	m, _ = p.update(m, keyMsg("esc"))
	if p.maxed() || m.proc == nil {
		t.Fatalf("esc on a maximized list: maxed=%v proc=%v, want restored and still in the process", p.maxed(), m.proc != nil)
	}
	m, _ = p.update(m, keyMsg("esc"))
	if m.proc != nil {
		t.Error("second esc did not leave the process")
	}
}

// The hint advertises the key.
func TestConflictListHintAdvertisesCtrlT(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40}
	out := ansi.Strip(conflictListBox(m, conflictListFixture(2), 0, domain.ConflictState{}, "", modeCutoff, 0, false))
	if !strings.Contains(out, "[ctrl+t] full") {
		t.Errorf("hint lacks [ctrl+t] full:\n%s", out)
	}
}
