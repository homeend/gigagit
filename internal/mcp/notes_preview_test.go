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

// gg_notes_apply with a preview target SKIPS a batch's old-side item rather
// than refusing the whole batch (spec §1.5, controller ruling: parity with
// the CLI's `gg note apply --preview`, which warns to stderr and keeps
// going): the new-side item is stored, skipped reports 1, warning carries
// the CLI's warnSkippedOldSide preview wording, and the reload post fires
// because something WAS stored.
func TestNotesApplyWithAPreviewSkipsAnOldSideItem(t *testing.T) {
	e := newTestEnv(t)
	seedPreviewBranch(t, e)

	e.srv.steerDir = t.TempDir()
	if err := steer.Touch(e.srv.steerDir, steer.TUIPresence, steer.Presence{PID: 1, Worktree: e.dir}); err != nil {
		t.Fatal(err)
	}

	out := e.call(t, "gg_notes_apply", map[string]any{
		"preview": "main...feat",
		"batch": json.RawMessage(`{"comments":[
			{"filePath":"a.txt","newLine":1,"summary":"good, stored"},
			{"filePath":"a.txt","oldLine":1,"summary":"old side: skipped"}]}`),
	})
	notes, _ := out["notes"].([]any)
	if len(notes) != 1 {
		t.Fatalf("notes = %v, want exactly the one new-side item stored", out["notes"])
	}
	wire, _ := notes[0].(map[string]any)
	if wire["summary"] != "good, stored" {
		t.Fatalf("stored note = %v, want summary \"good, stored\"", wire)
	}
	if skipped, _ := out["skipped"].(float64); skipped != 1 {
		t.Fatalf("skipped = %v, want 1", out["skipped"])
	}
	warning, _ := out["warning"].(string)
	if !strings.Contains(warning, "notes in a preview anchor on the new side") {
		t.Fatalf("warning = %q, want the preview old-side wording", warning)
	}

	if got := steer.Drain(e.srv.steerDir); len(got) == 0 {
		t.Fatalf("a batch that stored something must still fire the notes-changed reload post")
	}

	list := e.call(t, "gg_notes_list", map[string]any{"preview": "main...feat", "file": "a.txt"})
	if listed, _ := list["notes"].([]any); len(listed) != 1 {
		t.Fatalf("gg_notes_list = %v, want the one stored new-side note", list["notes"])
	}
}

// gg_notes_list with a preview target and no file gathers every path's notes
// in one store load (domain.PreviewNotesAll), not one PreviewNotesAt call per
// path — covered here functionally: notes on two different files both come
// back from a single --file-less list call.
func TestNotesListWithAPreviewAndNoFileGathersEveryPath(t *testing.T) {
	e := newTestEnv(t)
	seedPreviewBranch(t, e)
	gitRun(t, e.dir, "checkout", "feat")
	if err := os.WriteFile(filepath.Join(e.dir, "b.txt"), []byte("bee\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, e.dir, "add", "-A")
	gitRun(t, e.dir, "commit", "-m", "feat: add b.txt")
	gitRun(t, e.dir, "checkout", "main")

	e.call(t, "gg_note_add", map[string]any{
		"preview": "main...feat", "file": "a.txt", "new_line": 2, "summary": "about a",
	})
	e.call(t, "gg_note_add", map[string]any{
		"preview": "main...feat", "file": "b.txt", "new_line": 1, "summary": "about b",
	})

	out := e.call(t, "gg_notes_list", map[string]any{"preview": "main...feat"})
	notes, _ := out["notes"].([]any)
	if len(notes) != 2 {
		t.Fatalf("notes = %v, want both a.txt and b.txt notes", out["notes"])
	}
	var summaries []string
	for _, n := range notes {
		wire, _ := n.(map[string]any)
		summaries = append(summaries, wire["summary"].(string))
	}
	if !strings.Contains(strings.Join(summaries, ","), "about a") || !strings.Contains(strings.Join(summaries, ","), "about b") {
		t.Fatalf("summaries = %v, want both about a and about b", summaries)
	}
}

// gg_note_add with a preview target normalises the file path through
// NoteTarget, exactly like every other target: "./a.txt" is stored as
// "a.txt", and a path escaping the repository ("../a.txt") is a usage error
// — mirrors the CLI's `note add --preview` (internal/cli/note.go).
func TestNoteAddWithAPreviewNormalisesThePath(t *testing.T) {
	e := newTestEnv(t)
	tip, _ := seedPreviewBranch(t, e)

	out := e.call(t, "gg_note_add", map[string]any{
		"preview": "main...feat", "file": "./a.txt", "new_line": 2, "summary": "dotted path",
	})
	note, _ := out["note"].(map[string]any)
	id, _ := note["id"].(string)
	if id == "" {
		t.Fatalf("gg_note_add reply = %v", out)
	}
	stored, err := e.svc.NoteGet(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	wantAddr := model.FileAddress{State: model.StateCommitted, Commit: tip, Path: "a.txt"}
	if stored.Address != wantAddr {
		t.Fatalf("address = %+v, want %+v (./a.txt normalised)", stored.Address, wantAddr)
	}

	msg := e.callErr(t, "gg_note_add", map[string]any{
		"preview": "main...feat", "file": "../a.txt", "new_line": 1, "summary": "escapes",
	})
	if !strings.Contains(msg, "escapes the repository") {
		t.Fatalf("msg = %q, want the path-escape usage error", msg)
	}
}

// A non-OK preview pair names its state word (missing source/target, merged,
// no base) rather than one coarse "not previewable" message — mirrors the
// CLI's resolvePreviewTarget (internal/cli/previewflag.go).
func TestPreviewSetNamesTheStateWord(t *testing.T) {
	e := newTestEnv(t)
	seedPreviewBranch(t, e)

	msg := e.callErr(t, "gg_notes_list", map[string]any{"preview": "main...no-such-branch"})
	if !strings.Contains(msg, "missing: no-such-branch") {
		t.Fatalf("msg = %q, want the missing-source state word", msg)
	}
}

// hunk and new_line are MUTUALLY EXCLUSIVE under preview too: the ordinary
// arm (noteAnchor) and the CLI both refuse two anchors, so the preview arm
// must not silently prefer one of them and store a note at a line the caller
// did not mean. Nothing may be stored on the way out.
func TestNoteAddPreviewRefusesTwoAnchors(t *testing.T) {
	e := newTestEnv(t)
	seedPreviewBranch(t, e)

	msg := e.callErr(t, "gg_note_add", map[string]any{
		"preview": "main...feat", "file": "a.txt", "new_line": 2, "hunk": 1, "summary": "ambiguous",
	})
	if !strings.Contains(msg, "exactly one of hunk or new_line") {
		t.Fatalf("msg = %q, want the exactly-one usage error", msg)
	}

	out := e.call(t, "gg_notes_list", map[string]any{"preview": "main...feat"})
	if notes, _ := out["notes"].([]any); len(notes) != 0 {
		t.Fatalf("a refused add must store nothing, got %v", out["notes"])
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
