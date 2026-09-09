package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

// noteBatchRepo is a real repo with a committed file and a one-line working
// change (a single hunk) — enough to exercise PlanNoteBatch/ApplyNoteBatch
// against real content, mirroring internal/cli's noteRepo fixture.
func noteBatchRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\nbravo\ncharlie\ndelta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "a.txt")
	gittest.Run(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\nBRAVO\ncharlie\ndelta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// noteBatchSvc builds a Service over a real noteBatchRepo, with its own
// isolated notes store (never the machine's real XDG state dir).
func noteBatchSvc(t *testing.T) *Service {
	t.Helper()
	svc := svcIn(t, noteBatchRepo(t))
	svc.UseNotesDir(t.TempDir())
	return svc
}

// PlanNoteBatch with NoteSideNewOnly (a range/working review's rule, per
// §4.4's old-side table): an old-side item is dropped and counted in
// skipped, a new-side item is planned normally.
func TestPlanNoteBatchSideRuleNewOnlySkipsOldSideItems(t *testing.T) {
	svc := noteBatchSvc(t)
	ctx := context.Background()

	b := notebatch.Batch{Items: []notebatch.Item{
		{Path: "a.txt", Target: notebatch.Target{OldLine: [2]int{1, 1}}, Summary: "old side, dropped"},
		{Path: "a.txt", Target: notebatch.Target{NewLine: [2]int{1, 1}}, Summary: "new side, planned"},
	}}
	planned, skipped, err := svc.PlanNoteBatch(ctx, b, false, "", "agent", NoteSideNewOnly)
	if err != nil {
		t.Fatalf("PlanNoteBatch: %v", err)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	if len(planned) != 1 || planned[0].Note.Side != model.NoteSideNew || planned[0].Note.Summary != "new side, planned" {
		t.Fatalf("planned = %+v, want exactly the new-side item", planned)
	}
}

// ApplyNoteBatch must roll back what it already stored when a LATER item's
// write fails — here, a reply whose parent was removed after PlanNoteBatch
// validated it but before ApplyNoteBatch got to it (another client racing the
// same store). Nothing this batch stored may survive the failure.
func TestApplyNoteBatchRollsBackOnMidBatchFailure(t *testing.T) {
	svc := noteBatchSvc(t)
	ctx := context.Background()

	addr, err := svc.NoteTarget(ctx, "a.txt", false, "")
	if err != nil {
		t.Fatalf("NoteTarget: %v", err)
	}
	root, err := svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceAgent, Author: "agent", Address: addr,
		Side: model.NoteSideNew, Range: [2]int{1, 1}, Summary: "root",
	})
	if err != nil {
		t.Fatalf("NoteAdd(root): %v", err)
	}

	in := `{"comments":[{"filePath":"a.txt","newLine":2,"summary":"good, stored first"},
	                    {"replyTo":"` + root.ID + `","summary":"orphaned before apply"}]}`
	batch, err := notebatch.Parse([]byte(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	planned, _, err := svc.PlanNoteBatch(ctx, batch, false, "", "agent", NoteSideBoth)
	if err != nil {
		t.Fatalf("PlanNoteBatch: %v", err)
	}
	// The parent disappears AFTER planning but BEFORE applying.
	if err := svc.NoteRemove(ctx, root.ID); err != nil {
		t.Fatalf("NoteRemove(root): %v", err)
	}

	if _, err := svc.ApplyNoteBatch(ctx, planned); err == nil {
		t.Fatal("ApplyNoteBatch must fail once the reply's parent is gone")
	} else if !strings.Contains(err.Error(), "item 1") || !strings.Contains(err.Error(), "rolled back 1 notes") {
		t.Fatalf("err = %q, want it to name the failing item and the rollback count", err)
	}
	list, err := svc.NotesAt(ctx, addr)
	if err != nil {
		t.Fatalf("NotesAt: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("a mid-batch failure must roll back everything this batch stored: %+v", list)
	}
}
