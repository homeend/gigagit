package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

var twoFiles = []steer.OpenFile{
	{ID: "f3", Path: "src/a.go", Source: "worktree", Line: 12, State: "shown"},
	{ID: "f1", Path: "b.txt", Source: "commit", Rev: "0123456789abcdef0123456789abcdef01234567", State: "background"},
}

func TestSessionFilesPrintsRows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	seen := answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Files: twoFiles} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"files"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	want := "f3\tsrc/a.go\tworktree\t:12\tshown\nf1\tb.txt\tcommit 0123456\t-\tbackground\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if c := <-seen; c.Cmd != "files" || !c.Wait {
		t.Errorf("posted %+v, want a waited files command", c)
	}
}

func TestSessionFilesJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Files: twoFiles} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"files", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	var got []steer.OpenFile
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || len(got) != 2 || got[0] != twoFiles[0] {
		t.Fatalf("stdout = %s err=%v", out.String(), err)
	}
}

func TestSessionFilesEmptyJSONIsAnArray(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "no open files"} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"files", "--json"}, &out, &errb); code != 0 || strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("exit=%d stdout=%q", code, out.String())
	}
}

func TestSessionFilesFocusPostsIDOrPathAndLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		arg      string
		id, path string
		line     int
	}{
		{"f3", "f3", "", 0},
		{"f3:40", "f3", "", 40},
		{"src/a.go", "", "src/a.go", 0},
		{"src/a.go:7", "", "src/a.go", 7},
		{"dir:x/a.go", "", "dir:x/a.go", 0},
	} {
		dir := t.TempDir()
		livePresence(t, dir)
		seen := answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "focused"} })
		var out, errb bytes.Buffer
		if code := runSession(dir, nil, []string{"files", "focus", tc.arg}, &out, &errb); code != 0 {
			t.Fatalf("%s: exit = %d (stderr %q)", tc.arg, code, errb.String())
		}
		c := <-seen
		gotLine := 0
		if c.Line != nil {
			gotLine = c.Line.No
		}
		if c.Cmd != "file_focus" || c.FileID != tc.id || c.File != tc.path || gotLine != tc.line {
			t.Errorf("%s: posted %+v", tc.arg, c)
		}
	}
}

func TestSessionFilesFocusUnknownExitsOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, Error: "no open file f9"} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"files", "focus", "f9"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "no open file f9") {
		t.Fatalf("exit=%d stderr=%q", code, errb.String())
	}
}

func TestSessionFilesNeedsALiveTUI(t *testing.T) {
	t.Parallel()
	var out, errb bytes.Buffer
	if code := runSession(t.TempDir(), nil, []string{"files"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "no gg TUI session for this worktree") {
		t.Fatalf("exit=%d stderr=%q", code, errb.String())
	}
	dir := t.TempDir()
	web, srv := newSteerServer(t, 202, "")
	liveWebPresence(t, dir, srv.URL)
	errb.Reset()
	if code := runSession(dir, nil, []string{"files"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "gg web does not keep open files yet") {
		t.Fatalf("web only: exit=%d stderr=%q", code, errb.String())
	}
	if len(web.commands()) != 0 {
		t.Error("files was posted to the web page")
	}
}

func TestSessionFilesUsageErrorsExitTwo(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"files", "extra"},
		{"files", "focus"},
		{"files", "focus", "a", "b"},
		{"files", "--no-wait"},
		{"files", "focus", "f1:0"},
	} {
		var out, errb bytes.Buffer
		if code := runSession(t.TempDir(), nil, args, &out, &errb); code != 2 {
			t.Errorf("%v: exit = %d (stderr %q), want 2", args, code, errb.String())
		}
	}
}

func TestSessionFilesOldTUIUnknownCommandExitsOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, Error: `unknown command "files"`} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"files"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), `unknown command "files"`) {
		t.Fatalf("exit=%d stderr=%q", code, errb.String())
	}
}

func TestSessionNavigateBackgroundPostsToTheTUI(t *testing.T) {
	t.Parallel()
	repo := newCLIRepo(t)
	dir := t.TempDir()
	livePresence(t, dir)
	seen := answer(t, dir, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: true, Detail: "opened README.md in the background"}
	})
	link := "gg://" + filepath.ToSlash(repo) + "/README.md?view=content"
	var out, errb bytes.Buffer
	code := runSession(dir, domain.Open(repo), []string{"navigate", "--background", link}, &out, &errb)
	if code != 0 || !strings.Contains(out.String(), "opened README.md in the background") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, out.String(), errb.String())
	}
	if c := <-seen; !c.Background || c.File != "README.md" || c.HintKind != model.ContentHintKind {
		t.Errorf("posted %+v", c)
	}
}

func TestSessionNavigateBackgroundRefusals(t *testing.T) {
	t.Parallel()
	repo := newCLIRepo(t)
	svc := domain.Open(repo)
	diffLink := "gg://" + filepath.ToSlash(repo) + "/README.md"
	for _, args := range [][]string{
		{"navigate", "--background", diffLink},
		{"navigate", "--background", "--file", "README.md", "--new-line", "1"},
	} {
		var out, errb bytes.Buffer
		if code := runSession(t.TempDir(), svc, args, &out, &errb); code != 2 {
			t.Errorf("%v: exit=%d stderr=%q, want 2", args, code, errb.String())
		}
	}
	content := "gg://" + filepath.ToSlash(repo) + "/README.md?view=content"
	var out, errb bytes.Buffer
	if code := runSession(t.TempDir(), svc, []string{"navigate", "--background", content}, &out, &errb); code != 1 ||
		!strings.Contains(errb.String(), "no gg TUI session for this worktree") {
		t.Fatalf("no TUI: exit=%d stderr=%q", code, errb.String())
	}
}
