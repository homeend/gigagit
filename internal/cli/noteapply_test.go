package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

func TestNoteApplyAgentContextShape(t *testing.T) {
	dir := noteRepo(t)
	in := `{"version":1,"summary":"overall fine","files":[{"path":"a.txt","summary":"scoring",
	  "annotations":[{"newRange":[2,2],"summary":"shouty","rationale":"why"},
	                 {"newRange":[3,4],"summary":"span note"}]}]}`
	code, out, errb := runCLIStdin(t, dir, in, "note", "apply", "--stdin")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	ids := strings.Fields(strings.TrimSpace(out))
	if len(ids) != 2 {
		t.Fatalf("stdout = %q, want one id per stored note", out)
	}
	// Unanchored prose is CONTEXT: echoed, never stored.
	if !strings.Contains(errb, "context: overall fine") || !strings.Contains(errb, "context: a.txt: scoring") {
		t.Fatalf("stderr = %q, want the top-level and file summaries as context lines", errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if !strings.Contains(list, "new:3-4") {
		t.Fatalf("a multi-line newRange must anchor the whole span:\n%s", list)
	}
}

func TestNoteApplyCommentShapeWithHunkAndReply(t *testing.T) {
	dir := noteRepo(t)
	_, out, _ := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2", "--summary", "root")
	root := strings.TrimSpace(out)
	in := `{"comments":[{"filePath":"a.txt","hunk":1,"summary":"whole hunk"},
	                    {"replyTo":"` + root + `","summary":"addressed"}]}`
	code, out, errb := runCLIStdin(t, dir, in, "note", "apply", "--stdin", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	var wires []struct {
		ParentID string `json:"parent_id"`
		Summary  string `json:"summary"`
		Source   string `json:"source"`
	}
	if err := json.Unmarshal([]byte(out), &wires); err != nil {
		t.Fatalf("--json must be an array of wire notes: %v\n%s", err, out)
	}
	if len(wires) != 2 {
		t.Fatalf("wires = %+v, want 2", wires)
	}
	for _, w := range wires {
		if w.Source != "agent" {
			t.Errorf("every batch note is source agent: %+v", w)
		}
	}
	if wires[1].ParentID != root {
		t.Errorf("replyTo must produce a reply on the named root: %+v", wires[1])
	}
}

func TestNoteApplyAuthorFallbackAndOverride(t *testing.T) {
	dir := noteRepo(t)
	in := `{"files":[{"path":"a.txt","annotations":[
	  {"newRange":[1,1],"summary":"item author","author":"sonnet"},
	  {"newRange":[2,2],"summary":"flag author"}]}]}`
	code, out, errb := runCLIStdin(t, dir, in, "note", "apply", "--stdin", "--author", "reviewer", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, `"author":"sonnet"`) || !strings.Contains(out, `"author":"reviewer"`) {
		t.Fatalf("an item author wins; --author fills the rest: %s", out)
	}
}

// A batch that does not fully validate must store NOTHING.
func TestNoteApplyRejectsWholeBatchOnOneBadItem(t *testing.T) {
	dir := noteRepo(t)
	in := `{"files":[{"path":"a.txt","annotations":[
	  {"newRange":[1,1],"summary":"good"},
	  {"newRange":[2,2]}]}]}`
	code, _, errb := runCLIStdin(t, dir, in, "note", "apply", "--stdin")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, "annotations[1]") {
		t.Fatalf("stderr = %q, want the offending item named", errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if strings.TrimSpace(list) != "" {
		t.Fatalf("a rejected batch must store nothing, got:\n%s", list)
	}
}

// A range past the end of the file (unlike a missing summary) parses fine —
// notebatch has no idea how long the file is. planNoteBatch must catch it in
// its OWN validation pass, before applyNoteBatch writes anything: the first
// item here is perfectly valid and must NOT already be stored when the
// second item's out-of-range anchor fails the batch.
func TestNoteApplyRejectsWholeBatchOnOutOfRangeItem(t *testing.T) {
	dir := noteRepo(t)
	in := `{"files":[{"path":"a.txt","annotations":[
	  {"newRange":[1,1],"summary":"good, would store first"},
	  {"newRange":[100,100],"summary":"past the end of a 4-line file"}]}]}`
	code, _, errb := runCLIStdin(t, dir, in, "note", "apply", "--stdin")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, "past the end") {
		t.Fatalf("stderr = %q, want the out-of-range anchor named", errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if strings.TrimSpace(list) != "" {
		t.Fatalf("the earlier, valid item must not have been stored either:\n%s", list)
	}
}

// An unknown replyTo is caught in the VALIDATION pass, before any write.
func TestNoteApplyUnknownReplyToStoresNothing(t *testing.T) {
	dir := noteRepo(t)
	in := `{"comments":[{"filePath":"a.txt","newLine":1,"summary":"good"},
	                    {"replyTo":"ffffffff","summary":"orphan"}]}`
	code, _, errb := runCLIStdin(t, dir, in, "note", "apply", "--stdin")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, "ffffffff") {
		t.Fatalf("stderr = %q, want the unknown parent id named", errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if strings.TrimSpace(list) != "" {
		t.Fatalf("nothing may be stored: %s", list)
	}
}

func TestNoteApplyRequiresStdinFlag(t *testing.T) {
	dir := noteRepo(t)
	code, _, errb := runCLIStdin(t, dir, `{"files":[]}`, "note", "apply")
	if code != 2 || !strings.Contains(errb, "--stdin") {
		t.Fatalf("exit=%d stderr=%q, want 2 + a --stdin hint", code, errb)
	}
}

// A hunk number past the end of the file (unlike a missing summary) is only
// discoverable by resolving the diff — notebatch itself has no idea how many
// hunks a.txt has. Like the newRange case, this must fail the WHOLE batch
// before anything is written, not just the offending item.
func TestNoteApplyRejectsWholeBatchOnOutOfRangeHunk(t *testing.T) {
	dir := noteRepo(t)
	in := `{"comments":[{"filePath":"a.txt","newLine":1,"summary":"good, would store first"},
	                    {"filePath":"a.txt","hunk":2,"summary":"a.txt has only 1 hunk"}]}`
	code, _, errb := runCLIStdin(t, dir, in, "note", "apply", "--stdin")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, "has 1 hunks") {
		t.Fatalf("stderr = %q, want the file's hunk count named", errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if strings.TrimSpace(list) != "" {
		t.Fatalf("the earlier, valid item must not have been stored either:\n%s", list)
	}
}

// planNoteBatch with sideRuleNewOnly (a range/working review's rule, per
// §4.4's old-side table): an old-side item is dropped and counted in
// skipped, a new-side item is planned normally.
func TestPlanNoteBatchSideRuleNewOnlySkipsOldSideItems(t *testing.T) {
	dir := noteRepo(t)
	svc := domain.Open(dir)
	ctx := context.Background()

	b := notebatch.Batch{Items: []notebatch.Item{
		{Path: "a.txt", Target: notebatch.Target{OldLine: [2]int{1, 1}}, Summary: "old side, dropped"},
		{Path: "a.txt", Target: notebatch.Target{NewLine: [2]int{1, 1}}, Summary: "new side, planned"},
	}}
	planned, skipped, err := planNoteBatch(ctx, svc, b, false, "", "agent", sideRuleNewOnly)
	if err != nil {
		t.Fatalf("planNoteBatch: %v", err)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	if len(planned) != 1 || planned[0].Note.Side != model.NoteSideNew || planned[0].Note.Summary != "new side, planned" {
		t.Fatalf("planned = %+v, want exactly the new-side item", planned)
	}
}

// applyNoteBatch must roll back what it already stored when a LATER item's
// write fails — here, a reply whose parent was removed after planNoteBatch
// validated it but before applyNoteBatch got to it (another client racing the
// same store). Nothing this batch stored may survive the failure.
func TestApplyNoteBatchRollsBackOnMidBatchFailure(t *testing.T) {
	dir := noteRepo(t)
	svc := domain.Open(dir)
	ctx := context.Background()

	_, out, _ := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "root")
	root := strings.TrimSpace(out)

	in := `{"comments":[{"filePath":"a.txt","newLine":2,"summary":"good, stored first"},
	                    {"replyTo":"` + root + `","summary":"orphaned before apply"}]}`
	batch, err := notebatch.Parse([]byte(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	planned, _, err := planNoteBatch(ctx, svc, batch, false, "", "agent", sideRuleBoth)
	if err != nil {
		t.Fatalf("planNoteBatch: %v", err)
	}
	// The parent disappears AFTER planning but BEFORE applying.
	if err := svc.NoteRemove(ctx, root); err != nil {
		t.Fatalf("NoteRemove(root): %v", err)
	}

	if _, err := applyNoteBatch(ctx, svc, planned); err == nil {
		t.Fatal("applyNoteBatch must fail once the reply's parent is gone")
	} else if !strings.Contains(err.Error(), "item 1") || !strings.Contains(err.Error(), "rolled back 1 notes") {
		t.Fatalf("err = %q, want it to name the failing item and the rollback count", err)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if strings.TrimSpace(list) != "" {
		t.Fatalf("a mid-batch failure must roll back everything this batch stored:\n%s", list)
	}
}
