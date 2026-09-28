package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
)

// reviewNoteModel is a launch-ready Model with its own note store and one
// review saved on HEAD; it returns the model and the review's note id.
func reviewNoteModel(t *testing.T, text string) (Model, string) {
	t.Helper()
	m := launchTestModel(t)
	m.svc.UseNotesDir(t.TempDir())
	tg, err := m.svc.BranchReviewTarget(context.Background(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := m.svc.SaveReview(context.Background(), domain.SaveReview{Target: tg, Agent: "Claude Code", Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return m, id
}

func viewerText(m Model) string {
	v := layerOf[*fileViewer](m)
	if v == nil {
		return ""
	}
	var b strings.Builder
	for _, l := range v.p.lines {
		b.WriteString(l.text + "\n")
	}
	return b.String()
}

func TestOpenReviewNoteShowsTheText(t *testing.T) {
	m, id := reviewNoteModel(t, "# Verdict\nship it")
	m, cmd := m.openReviewNote(id, "Review: HEAD")
	m = drainCmds(t, m, cmd)
	v := layerOf[*fileViewer](m)
	if v == nil || v.src.kind != srcNote || v.title() != "Review: HEAD" {
		t.Fatalf("viewer %+v", v)
	}
	if got := viewerText(m); !strings.Contains(got, "ship it") {
		t.Fatalf("viewer text:\n%s", got)
	}
}

func TestOpenReviewNoteDeleted(t *testing.T) {
	m, _ := reviewNoteModel(t, "x")
	m, cmd := m.openReviewNote("nope0000", "Review")
	m = drainCmds(t, m, cmd)
	if got := viewerText(m); !strings.Contains(got, "review deleted") {
		t.Fatalf("viewer text:\n%s", got)
	}
}

func TestTaskTabEnterOpensAReviewNote(t *testing.T) {
	m, _ := reviewNoteModel(t, "x")
	tg, err := m.svc.BranchReviewTarget(context.Background(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := m.svc.ReviewTask(context.Background(), captureCmd(exttool.CatReview, "echo from-the-agent"), tg)
	if err != nil {
		t.Fatal(err)
	}
	id := domain.Tasks().Submit(spec)
	info := waitTaskState(t, id, taskEndedFn)
	if info.NoteID == "" {
		t.Fatalf("the review was not stored: %+v", info)
	}
	m, _ = m.openSessionsPopupOn(tabTasks, id)
	m, cmd := updateKey(m, "enter")
	m = drainCmds(t, m, cmd)
	if v := layerOf[*fileViewer](m); v == nil || v.src.kind != srcNote || v.src.rev != info.NoteID {
		t.Fatalf("viewer %+v", v)
	}
	if got := viewerText(m); !strings.Contains(got, "from-the-agent") {
		t.Fatalf("viewer text:\n%s", got)
	}
}
