package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

var twoNotes = []steer.FileNote{
	{ID: "t3", FileID: "f1", Path: "src/a.go", Start: 10, End: 12, Summary: "first", Author: "agent"},
	{ID: "t4", FileID: "f1", Path: "src/a.go", Start: 40, End: 40, Summary: "second", Author: "agent", Outdated: true},
}

func TestSessionNoteAddPostsTheRangeAndText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		arg        string
		id, path   string
		start, end int
	}{
		{"src/a.go:10-12", "", "src/a.go", 10, 12},
		{"src/a.go:7", "", "src/a.go", 7, 7},
		{"f3:7-9", "f3", "", 7, 9},
		{"dir:x/a.go:5", "", "dir:x/a.go", 5, 5},
	} {
		dir := t.TempDir()
		livePresence(t, dir)
		seen := answer(t, dir, func(c steer.Command) steer.Reply {
			return steer.Reply{ID: c.ID, OK: true, Detail: "noted x as t9", Notes: twoNotes[:1]}
		})
		var out, errb bytes.Buffer
		args := []string{"note", "add", tc.arg, "--summary", "look", "--rationale", "why", "--author", "claude"}
		if code := runSession(dir, nil, args, &out, &errb); code != 0 {
			t.Fatalf("%s: exit = %d (stderr %q)", tc.arg, code, errb.String())
		}
		c := <-seen
		if c.Cmd != "note_add" || c.FileID != tc.id || c.File != tc.path || c.Start != tc.start || c.End != tc.end ||
			c.Summary != "look" || c.Rationale != "why" || c.Author != "claude" || !c.Wait {
			t.Errorf("%s: posted %+v", tc.arg, c)
		}
		if strings.TrimSpace(out.String()) != "noted x as t9" {
			t.Errorf("%s: stdout = %q", tc.arg, out.String())
		}
	}
}

func TestSessionNoteAddJSONPrintsTheNote(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Notes: twoNotes[:1]} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"note", "add", "a.go:1", "--summary", "s", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	var got steer.FileNote
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.ID != "t3" {
		t.Fatalf("stdout = %s err=%v", out.String(), err)
	}
}

func TestSessionNoteListPrintsRowsAndJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	seen := answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Notes: twoNotes} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"note", "list", "src/a.go"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	want := "t3\tsrc/a.go\t10-12\tfirst\nt4\tsrc/a.go\t40-40\tsecond (outdated)\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if c := <-seen; c.Cmd != "note_list" || c.File != "src/a.go" {
		t.Errorf("posted %+v", c)
	}

	dir2 := t.TempDir()
	livePresence(t, dir2)
	answer(t, dir2, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "no notes"} })
	out.Reset()
	if code := runSession(dir2, nil, []string{"note", "list", "--json"}, &out, &errb); code != 0 || strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("empty --json: exit=%d stdout=%q", code, out.String())
	}
}

func TestSessionNoteShowPrintsTheNoteAndItsLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	n := steer.FileNote{ID: "t3", FileID: "f1", Path: "src/a.go", Start: 10, End: 11, Summary: "first", Rationale: "because\nof this", Author: "agent", Text: []string{"x := 1", "y := 2"}}
	seen := answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Notes: []steer.FileNote{n}} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"note", "show", "t3"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	want := "t3\tsrc/a.go:10-11\tagent\nfirst\n\nbecause\nof this\n\n10\tx := 1\n11\ty := 2\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if c := <-seen; c.Cmd != "note_show" || c.NoteID != "t3" {
		t.Errorf("posted %+v", c)
	}
}

func TestSessionNoteRmAndClear(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	seen := answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "removed t3"} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"note", "rm", "t3"}, &out, &errb); code != 0 || strings.TrimSpace(out.String()) != "removed t3" {
		t.Fatalf("rm: exit=%d stdout=%q stderr=%q", code, out.String(), errb.String())
	}
	if c := <-seen; c.Cmd != "note_rm" || c.NoteID != "t3" || c.File != "" {
		t.Errorf("rm posted %+v", c)
	}

	dir2 := t.TempDir()
	livePresence(t, dir2)
	seen2 := answer(t, dir2, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: true, Detail: "removed 2 notes from a.go"}
	})
	if code := runSession(dir2, nil, []string{"note", "clear", "f1"}, &out, &errb); code != 0 {
		t.Fatalf("clear: exit=%d stderr=%q", code, errb.String())
	}
	if c := <-seen2; c.Cmd != "note_rm" || c.NoteID != "" || c.FileID != "f1" {
		t.Errorf("clear posted %+v", c)
	}
}

func TestSessionNoteRefusedExitsOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, Error: "no note t9"} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"note", "show", "t9"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "no note t9") {
		t.Fatalf("exit=%d stderr=%q", code, errb.String())
	}
}

func TestSessionNoteNeedsALiveTUI(t *testing.T) {
	t.Parallel()
	var out, errb bytes.Buffer
	if code := runSession(t.TempDir(), nil, []string{"note", "list"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "no gg session for this worktree") {
		t.Fatalf("nothing live: exit=%d stderr=%q", code, errb.String())
	}
	dir := t.TempDir()
	liveWebPresence(t, dir, "http://127.0.0.1:1") // only gg web: never posted to
	errb.Reset()
	if code := runSession(dir, nil, []string{"note", "list"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "temporary notes need a gg TUI") {
		t.Fatalf("web only: exit=%d stderr=%q", code, errb.String())
	}
}

func TestSessionNoteUsageErrorsExitTwo(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"note"},
		{"note", "bogus"},
		{"note", "add", "a.go:1"}, // no --summary
		{"note", "add", "a.go", "--summary", "s"},     // no line
		{"note", "add", "a.go:0", "--summary", "s"},   // 0 is not a line
		{"note", "add", "a.go:5-3", "--summary", "s"}, // backwards
		{"note", "show"},
		{"note", "show", "a.go"}, // not a note id
		{"note", "rm", "f1"},     // rm takes a note id; clear takes a file
		{"note", "clear"},
		{"note", "list", "a", "b"},
	} {
		var out, errb bytes.Buffer
		if code := runSession(t.TempDir(), nil, args, &out, &errb); code != 2 {
			t.Errorf("%v: exit = %d, want 2 (stderr %q)", args, code, errb.String())
		}
	}
}
