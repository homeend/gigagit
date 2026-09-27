package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestWithReviewLinesPutsReviewsFirst(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 9, 27, 10, 0, 0, 0, time.Local)
	files := []contentLine{{text: ".github/", heading: true}, {text: "  M  x.yml", path: ".github/x.yml", status: "M"}}
	lines := withReviewLines([]domain.Review{{ID: "ab12cd34", Created: when}}, files)
	if len(lines) != 4 || lines[0].text != "@notes/" || !lines[0].heading {
		t.Fatalf("lines %+v, want the @notes/ heading first", lines)
	}
	if lines[1].noteID != "ab12cd34" || lines[1].path != "@notes/review-2026-09-27-ab12cd34.md" || lines[1].status != "R" {
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
	if len(vis) < 2 || vis[0].text != "@notes/" || vis[1].noteID == "" {
		t.Fatalf("file list does not start with the review: %+v", vis)
	}
}

func TestReviewEntryOpensAsAnAddedFile(t *testing.T) {
	t.Parallel()
	m := reviewedStackModel(t, "# Verdict\nship it")
	u, cmd := m.openDiffForFileLine(m.filesView.visible()[1])
	m = drainCmds(t, u.(Model), cmd)
	v := m.diffLayer()
	if v == nil || v.err != nil {
		t.Fatalf("diff %+v", v)
	}
	if out := strings.Join(diffRowTexts(v), "\n"); !strings.Contains(out, "ship it") {
		t.Fatalf("the review text is not in the diff:\n%s", out)
	}
	if v.noteAddr.Commit != "" || v.noteAddr.Path != "" {
		t.Fatalf("a review must not be note-addressable: %+v", v.noteAddr)
	}
}

func TestStackShowsTheReviewAboveTheFiles(t *testing.T) {
	t.Parallel()
	m := reviewedStackModel(t, "# Verdict\nship it")
	m = m.setStackedPref(true)
	u, cmd := m.openDiffForFileLine(m.filesView.visible()[1])
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
