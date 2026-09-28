package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
)

func TestWithReviewLinesPutsReviewsFirst(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 9, 27, 10, 0, 0, 0, time.Local)
	files := []contentLine{{text: ".github/", heading: true}, {text: "  M  x.yml", path: ".github/x.yml", status: "M"}}
	lines := withReviewLines([]domain.Review{{ID: "ab12cd34", Created: when, Agent: "Claude Code", Summary: "Review: feature"}}, files)
	if len(lines) != 4 || lines[0].text != "Reviews" || !lines[0].heading {
		t.Fatalf("lines %+v, want the Reviews heading first", lines)
	}
	// A review reads as one — its date, agent and title — not as a file.
	if lines[1].noteID != "ab12cd34" || lines[1].text != "  ◆ 2026-09-27 10:00 · Claude Code · Review: feature" {
		t.Fatalf("review line %+v", lines[1])
	}
	if got := withReviewLines(nil, files); len(got) != 2 {
		t.Fatalf("no reviews must leave the list alone: %+v", got)
	}
}

// reviewedStackModel is stackRepoModel with a review saved on the commit and
// the commit's file list reopened, so its @notes/ entry is there.
func reviewedStackModel(t *testing.T, text string) Model {
	t.Helper()
	m := stackRepoModel(t)
	m.svc.UseNotesDir(t.TempDir())
	hash := m.commits[0].Hash
	tg := domain.ReviewTarget{Kind: domain.ReviewRange, Range: hash + "^.." + hash, Commit: hash}
	if _, _, err := m.svc.SaveReview(context.Background(), domain.SaveReview{Target: tg, Agent: "Claude Code", Text: text}); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.openChangedFiles(m.commits[0])
	return drainCmds(t, m, cmd)
}

func TestCommitFilesListTheReview(t *testing.T) {
	t.Parallel()
	m := reviewedStackModel(t, "# Verdict\nship it")
	vis := m.filesView.visible()
	if len(vis) < 2 || vis[0].text != "Reviews" || vis[1].noteID == "" {
		t.Fatalf("file list does not start with the review: %+v", vis)
	}
}

// enter on an @notes/ entry opens the review, not a diff: a prose review in
// the markdown viewer (a structured one as the review view, review_view_test).
func TestReviewEntryOpensTheReview(t *testing.T) {
	t.Parallel()
	m := reviewedStackModel(t, "# Verdict\nship it")
	u, cmd := m.openDiffForFileLine(m.filesView.visible()[1])
	m = drainCmds(t, u.(Model), cmd)
	if m.diffLayer() != nil {
		t.Fatal("an @notes/ entry opens no diff")
	}
	if got := viewerText(m); !strings.Contains(got, "ship it") || strings.Contains(got, "# Verdict") {
		t.Fatalf("viewer text:\n%s", got)
	}
}

func TestStackShowsTheReviewAboveTheFiles(t *testing.T) {
	t.Parallel()
	m := reviewedStackModel(t, "# Verdict\nship it")
	m = m.setStackedPref(true)
	u, cmd := m.openDiffForFileLine(m.filesView.visible()[2]) // a.go: the stack still starts with the review
	m = drainCmds(t, u.(Model), cmd)
	v := m.diffLayer()
	if v == nil || v.stk == nil || len(v.stk.files) != 4 {
		t.Fatalf("stack %+v", v)
	}
	if v.stk.files[0].line.noteID == "" || v.stk.files[0].load != stackLoaded {
		t.Fatalf("first stack file %+v, want the loaded review", v.stk.files[0])
	}
}

func diffRowTexts(v *diffView) []string {
	out := make([]string, 0, len(v.full))
	for _, r := range v.full {
		out = append(out, r.Right)
	}
	return out
}

func TestReviewEntryActions(t *testing.T) {
	t.Parallel()
	m := reviewedStackModel(t, "# Verdict\nship it")
	m.filesTreeFocused = true
	m.filesView.sel = 1 // the review entry
	if r, ok := m.viewFileRow(); !ok || r.label != "View review" {
		t.Fatalf("view row %+v %v", r, ok)
	}
	if _, ok := m.commitsTouchingFileRow(); ok {
		t.Fatal("a review has no history to browse")
	}
	r, _ := m.viewFileRow()
	u, cmd := r.run(m)
	m = drainCmds(t, u.(Model), cmd)
	if m.filesPreview == nil || m.filesPreview.src.kind != srcNote {
		t.Fatalf("preview %+v", m.filesPreview)
	}
	var b strings.Builder
	for _, l := range m.filesPreview.p.lines {
		b.WriteString(l.text + "\n")
	}
	if !strings.Contains(b.String(), "ship it") {
		t.Fatalf("preview text:\n%s", b.String())
	}
}

// A review row is prose, not a path: cut at its end, never in the middle.
func TestReviewRowCutsAtItsEnd(t *testing.T) {
	t.Parallel()
	m := reviewedStackModel(t, "# Verdict\nship it")
	for i, l := range m.filesView.lines {
		if l.noteID != "" {
			m.filesView.lines[i].text = "  ◆ 2026-09-28 20:16 · Claude Code · Review: " + strings.Repeat("long title ", 20) + "END"
		}
	}
	m.filesTreeFocused = false // no reveal over the row
	view := ansi.Strip(m.View())
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "◆ 2026-09-28") {
			if strings.Contains(l, "END") || !strings.Contains(l, "· Claude Code ·") {
				t.Fatalf("review row cut in the middle: %q", l)
			}
			return
		}
	}
	t.Fatalf("no review row on screen:\n%s", view)
}
