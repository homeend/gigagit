package cli

import (
	"encoding/json"
	"strings"
	"testing"
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
