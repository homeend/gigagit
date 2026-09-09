package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedNoteFile makes a.txt differ from HEAD so the working tree has a diff to
// anchor notes against.
func seedNoteFile(t *testing.T, e *testEnv) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nWORLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNoteAddAndListTools(t *testing.T) {
	e := newTestEnv(t)
	seedNoteFile(t, e)
	out := e.call(t, "gg_note_add", map[string]any{
		"file": "a.txt", "new_line": 2, "summary": "shouty", "rationale": "why", "author": "sonnet",
	})
	note, ok := out["note"].(map[string]any)
	if !ok || note["summary"] != "shouty" || note["author"] != "sonnet" {
		t.Fatalf("gg_note_add reply = %v", out)
	}
	id, _ := note["id"].(string)
	if len(id) != 8 {
		t.Fatalf("note id = %q, want 8 hex", id)
	}

	list := e.call(t, "gg_notes_list", map[string]any{"file": "a.txt"})
	raw, err := json.Marshal(list["notes"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"summary":"shouty"`) || !strings.Contains(string(raw), `"status":`) {
		t.Fatalf("gg_notes_list must return resolved wire notes: %s", raw)
	}

	e.call(t, "gg_note_rm", map[string]any{"id": id})
	list = e.call(t, "gg_notes_list", map[string]any{"file": "a.txt"})
	raw, _ = json.Marshal(list["notes"])
	if strings.Contains(string(raw), "shouty") {
		t.Fatalf("gg_note_rm must remove the note: %s", raw)
	}
}

func TestNotesApplyToolIsAllOrNothing(t *testing.T) {
	e := newTestEnv(t)
	seedNoteFile(t, e)
	msg := e.callErr(t, "gg_notes_apply", map[string]any{
		"batch": json.RawMessage(`{"files":[{"path":"a.txt","annotations":[{"newRange":[1,1],"summary":"ok"},{"newRange":[2,2]}]}]}`),
	})
	if !strings.Contains(msg, "annotations[1]") {
		t.Fatalf("a bad item must name itself: %s", msg)
	}
	list := e.call(t, "gg_notes_list", map[string]any{"file": "a.txt"})
	raw, _ := json.Marshal(list["notes"])
	if strings.Contains(string(raw), `"ok"`) {
		t.Fatalf("a rejected batch must store nothing: %s", raw)
	}

	out := e.call(t, "gg_notes_apply", map[string]any{
		"batch":  json.RawMessage(`{"comments":[{"filePath":"a.txt","newLine":2,"summary":"batched"}]}`),
		"author": "reviewer",
	})
	raw, _ = json.Marshal(out["notes"])
	if !strings.Contains(string(raw), `"batched"`) || !strings.Contains(string(raw), `"author":"reviewer"`) {
		t.Fatalf("apply reply = %s", raw)
	}
}

// gg_notes_apply shares domain's PlanNoteBatch/ApplyNoteBatch with the CLI,
// which validates every item's RANGE (via domain.NoteRangeCheck) up front —
// not just its shape. A valid first item plus an out-of-range second item
// must store nothing, the same as `gg note apply --stdin`.
func TestNotesApplyToolRejectsOutOfRangeItem(t *testing.T) {
	e := newTestEnv(t)
	seedNoteFile(t, e) // a.txt is 2 lines on its new side
	msg := e.callErr(t, "gg_notes_apply", map[string]any{
		"batch": json.RawMessage(`{"files":[{"path":"a.txt","annotations":[
			{"newRange":[1,1],"summary":"good, would store first"},
			{"newRange":[100,100],"summary":"past the end of a 2-line file"}]}]}`),
	})
	if !strings.Contains(msg, "past the end") {
		t.Fatalf("msg = %q, want the out-of-range anchor named", msg)
	}
	if !strings.Contains(msg, "item 1") {
		t.Fatalf("msg = %q, want the offending item's index named", msg)
	}
	list := e.call(t, "gg_notes_list", map[string]any{"file": "a.txt"})
	raw, _ := json.Marshal(list["notes"])
	if strings.Contains(string(raw), "good, would store first") {
		t.Fatalf("the earlier, valid item must not have been stored either: %s", raw)
	}
}

// A comment batch's replyTo threads onto the named root in one call, and
// gg_notes_list resolves the thread with the reply nested under it — both
// notes Source: agent, matching the CLI's `gg note apply --stdin`.
func TestNotesApplyToolThreadsAReply(t *testing.T) {
	e := newTestEnv(t)
	seedNoteFile(t, e)
	root := e.call(t, "gg_note_add", map[string]any{
		"file": "a.txt", "new_line": 1, "summary": "root",
	})
	rootID, _ := root["note"].(map[string]any)["id"].(string)
	if rootID == "" {
		t.Fatalf("gg_note_add reply = %v", root)
	}

	e.call(t, "gg_notes_apply", map[string]any{
		"batch": json.RawMessage(`{"comments":[{"replyTo":"` + rootID + `","summary":"addressed"}]}`),
	})

	list := e.call(t, "gg_notes_list", map[string]any{"file": "a.txt"})
	notes, _ := list["notes"].([]any)
	if len(notes) != 1 {
		t.Fatalf("notes = %v, want exactly the one root thread", list["notes"])
	}
	rootWire, _ := notes[0].(map[string]any)
	if rootWire["source"] != "agent" {
		t.Fatalf("root = %v, want source agent", rootWire)
	}
	replies, _ := rootWire["replies"].([]any)
	if len(replies) != 1 {
		t.Fatalf("root replies = %v, want exactly one nested reply", rootWire["replies"])
	}
	reply, _ := replies[0].(map[string]any)
	if reply["summary"] != "addressed" || reply["source"] != "agent" {
		t.Fatalf("reply = %v, want summary addressed and source agent", reply)
	}
}

// gg_notes_apply hands back a batch's unanchored prose (agent-context v1's
// top-level/file summaries) as contexts — the same text the CLI echoes to
// stderr as "context: …" — instead of dropping it.
func TestNotesApplyToolReturnsContexts(t *testing.T) {
	e := newTestEnv(t)
	seedNoteFile(t, e)
	out := e.call(t, "gg_notes_apply", map[string]any{
		"batch": json.RawMessage(`{"version":1,"summary":"overall fine","files":[{"path":"a.txt","annotations":[{"newRange":[1,1],"summary":"ok"}]}]}`),
	})
	ctxs, ok := out["contexts"].([]any)
	if !ok || len(ctxs) != 1 || ctxs[0] != "overall fine" {
		t.Fatalf("contexts = %v, want [\"overall fine\"]", out["contexts"])
	}
}

func TestNoteAddToolRejectsRangeRev(t *testing.T) {
	e := newTestEnv(t)
	seedNoteFile(t, e)
	msg := e.callErr(t, "gg_note_add", map[string]any{
		"file": "a.txt", "new_line": 1, "rev": "main..HEAD", "summary": "s",
	})
	if !strings.Contains(msg, "one commit") {
		t.Fatalf("a range rev must be refused with the documented hint: %s", msg)
	}
}

// gg_notes_list's cached/rev flags only make sense against a single file
// target; without file, silently falling back to "every address this
// checkout can see" would answer a different question than the one asked
// (mirrors the CLI's `note list --cached` guard).
func TestNotesListCachedRevWithoutFileIsError(t *testing.T) {
	e := newTestEnv(t)
	seedNoteFile(t, e)
	for _, args := range []map[string]any{
		{"cached": true},
		{"rev": "HEAD"},
	} {
		msg := e.callErr(t, "gg_notes_list", args)
		if !strings.Contains(msg, "cached/rev need file") {
			t.Fatalf("args=%v msg=%q, want the cached/rev-need-file error", args, msg)
		}
	}
}

// An unknown type must fail loudly rather than silently return an empty
// list, which would look identical to "no notes here" to a caller that
// mistyped the value.
func TestNotesListUnknownTypeIsError(t *testing.T) {
	e := newTestEnv(t)
	seedNoteFile(t, e)
	e.call(t, "gg_note_add", map[string]any{"file": "a.txt", "new_line": 2, "summary": "s"})
	msg := e.callErr(t, "gg_notes_list", map[string]any{"file": "a.txt", "type": "bogus"})
	if !strings.Contains(msg, "type must be user, agent or all") {
		t.Fatalf("msg = %q, want the type usage message", msg)
	}
}

// gg_note_add and gg_notes_apply must honour $GG_AGENT for their author
// default, exactly like the CLI's `gg note add`/`gg note apply --stdin`
// (domain.NoteAuthorDefault is the shared implementation).
func TestNoteAddAndApplyHonourGGAgentAuthor(t *testing.T) {
	e := newTestEnv(t)
	seedNoteFile(t, e)
	t.Setenv("GG_AGENT", "sonnet")

	out := e.call(t, "gg_note_add", map[string]any{
		"file": "a.txt", "new_line": 1, "summary": "s",
	})
	note, _ := out["note"].(map[string]any)
	if note["author"] != "sonnet" {
		t.Fatalf("gg_note_add author = %v, want $GG_AGENT default sonnet", note["author"])
	}

	applied := e.call(t, "gg_notes_apply", map[string]any{
		"batch": json.RawMessage(`{"comments":[{"filePath":"a.txt","newLine":2,"summary":"batched"}]}`),
	})
	raw, _ := json.Marshal(applied["notes"])
	if !strings.Contains(string(raw), `"author":"sonnet"`) {
		t.Fatalf("gg_notes_apply notes = %s, want $GG_AGENT default sonnet", raw)
	}
}

func TestNoteToolAnnotations(t *testing.T) {
	e := newTestEnv(t)
	tools := e.listTools(t)
	for name, wantReadOnly := range map[string]bool{
		"gg_notes_list": true, "gg_note_add": false, "gg_notes_apply": false, "gg_note_rm": false,
	} {
		ann, ok := tools[name]
		if !ok {
			t.Errorf("tool %q is not registered", name)
			continue
		}
		if ann.ReadOnlyHint != wantReadOnly {
			t.Errorf("%s ReadOnlyHint = %v, want %v", name, ann.ReadOnlyHint, wantReadOnly)
		}
	}
}
