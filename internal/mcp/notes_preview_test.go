package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// seedPreviewBranch grows a feat branch off newTestEnv's one-commit main
// (a.txt: "hello\nworld\n"): older rewrites line 2 to WORLD, tip rewrites it
// again to EARTH — the same "second commit rewrites a line the first added"
// shape domain's newPreviewRepo uses, so a note anchored on older's own text
// goes stale (PreviewStatus: "outdated") once resolved against the tip.
func seedPreviewBranch(t *testing.T, e *testEnv) (tip, older string) {
	t.Helper()
	gitRun(t, e.dir, "checkout", "-b", "feat")
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nWORLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, e.dir, "add", "-A")
	gitRun(t, e.dir, "commit", "-m", "feat: WORLD")
	older = gitRun(t, e.dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nEARTH\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, e.dir, "add", "-A")
	gitRun(t, e.dir, "commit", "-m", "feat: EARTH")
	tip = gitRun(t, e.dir, "rev-parse", "HEAD")
	gitRun(t, e.dir, "checkout", "main")
	return tip, older
}

// gg_note_add with a preview target stores an ORDINARY committed note on the
// source TIP, new side only — never a new kind of stored address — and still
// fires the notes-changed reload post, exactly like a commit target.
func TestNoteAddWithAPreviewStoresOnTheTip(t *testing.T) {
	e := newTestEnv(t)
	tip, _ := seedPreviewBranch(t, e)

	// Safe to write before the call: New()'s only goroutine is the notes
	// sweep, which never reads steerDir (mirrors
	// TestNotesApplyStoringNothingPostsNoReload).
	e.srv.steerDir = t.TempDir()
	if err := steer.Touch(e.srv.steerDir, steer.TUIPresence, steer.Presence{PID: 1, Worktree: e.dir}); err != nil {
		t.Fatal(err)
	}

	out := e.call(t, "gg_note_add", map[string]any{
		"preview": "main...feat", "file": "a.txt", "new_line": 2, "summary": "on the tip",
	})
	note, ok := out["note"].(map[string]any)
	if !ok || note["summary"] != "on the tip" {
		t.Fatalf("gg_note_add reply = %v", out)
	}
	id, _ := note["id"].(string)
	if id == "" {
		t.Fatalf("note id missing: %v", note)
	}

	stored, err := e.svc.NoteGet(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	wantAddr := model.FileAddress{State: model.StateCommitted, Commit: tip, Path: "a.txt"}
	if stored.Address != wantAddr {
		t.Fatalf("address = %+v, want %+v", stored.Address, wantAddr)
	}
	if stored.Side != model.NoteSideNew {
		t.Fatalf("side = %q, want new", stored.Side)
	}

	if got := steer.Drain(e.srv.steerDir); len(got) == 0 {
		t.Fatalf("gg_note_add --preview must fire the notes-changed reload post like any other target")
	}
}

// gg_notes_list with a preview target reports "outdated" — the preview's word
// for a note whose anchored text a later commit changed — for a note stored
// on an OLDER commit on the branch, once resolved against the tip.
func TestNotesListWithAPreviewReportsOutdated(t *testing.T) {
	e := newTestEnv(t)
	_, older := seedPreviewBranch(t, e)

	// An ordinary committed note on the older commit, anchored to the text
	// ("WORLD") that the tip's own commit later rewrote to "EARTH".
	e.call(t, "gg_note_add", map[string]any{
		"rev": older, "file": "a.txt", "new_line": 2, "summary": "about WORLD",
	})

	out := e.call(t, "gg_notes_list", map[string]any{"preview": "main...feat", "file": "a.txt"})
	notes, _ := out["notes"].([]any)
	if len(notes) != 1 {
		t.Fatalf("notes = %v, want exactly the one note", out["notes"])
	}
	wire, _ := notes[0].(map[string]any)
	if wire["summary"] != "about WORLD" {
		t.Fatalf("wire note = %v, want summary about WORLD", wire)
	}
	if wire["status"] != "outdated" {
		t.Fatalf("status = %v, want outdated", wire["status"])
	}
}

// gg_notes_apply with a preview target refuses a batch carrying an old-side
// item: the preview's old side is the merge base, which no stored address
// names, so old-side items are refused with domain.ErrPreviewOldSide rather
// than silently skipped (unlike the CLI's --preview arm, which has stderr to
// warn on and keeps going) — and, being refused before ApplyNoteBatch runs,
// the batch stores NOTHING, including the otherwise-good new-side item.
func TestNotesApplyWithAPreviewRefusesAnOldSideItem(t *testing.T) {
	e := newTestEnv(t)
	seedPreviewBranch(t, e)

	msg := e.callErr(t, "gg_notes_apply", map[string]any{
		"preview": "main...feat",
		"batch": json.RawMessage(`{"comments":[
			{"filePath":"a.txt","newLine":1,"summary":"good, would store first"},
			{"filePath":"a.txt","oldLine":1,"summary":"old side: refused"}]}`),
	})
	if !strings.Contains(msg, "notes in a preview anchor on the new side") {
		t.Fatalf("msg = %q, want the domain old-side error", msg)
	}

	list := e.call(t, "gg_notes_list", map[string]any{"preview": "main...feat", "file": "a.txt"})
	if notes, _ := list["notes"].([]any); len(notes) != 0 {
		t.Fatalf("a refused batch must store nothing, including the earlier good item: %v", notes)
	}
}

// preview combined with rev (or cached) is a caller error, never a silent
// precedence rule — checked once here across gg_note_add; gg_notes_list and
// gg_notes_apply share the same previewSet helper.
func TestPreviewAndRevAreMutuallyExclusive(t *testing.T) {
	e := newTestEnv(t)
	seedPreviewBranch(t, e)

	msg := e.callErr(t, "gg_note_add", map[string]any{
		"preview": "main...feat", "rev": "main", "file": "a.txt", "new_line": 1, "summary": "s",
	})
	if !strings.Contains(msg, "one target only") {
		t.Fatalf("msg = %q, want the one-target-only usage error", msg)
	}

	msg = e.callErr(t, "gg_notes_list", map[string]any{"preview": "main...feat", "cached": true})
	if !strings.Contains(msg, "one target only") {
		t.Fatalf("gg_notes_list msg = %q, want the one-target-only usage error", msg)
	}

	msg = e.callErr(t, "gg_notes_apply", map[string]any{
		"preview": "main...feat", "rev": "main",
		"batch": json.RawMessage(`{"comments":[{"filePath":"a.txt","newLine":1,"summary":"s"}]}`),
	})
	if !strings.Contains(msg, "one target only") {
		t.Fatalf("gg_notes_apply msg = %q, want the one-target-only usage error", msg)
	}
}
