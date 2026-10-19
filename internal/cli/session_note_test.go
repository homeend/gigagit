package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// docsCLI is a repo with a.go (3 lines, LF) and sub/b.go (CRLF) committed,
// its service, and a temp inbox whose docs dir the verbs write.
func docsCLI(t *testing.T) (svc *domain.Service, inbox, dir string) {
	t.Helper()
	repo := newCLIRepo(t)
	write := func(rel, content string) {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package a\n\nfunc gate() {}\n")
	write("sub/b.go", "one\r\ntwo\r\nthree\r\n")
	write("logo.png", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00")
	inbox = t.TempDir()
	return domain.Open(repo), inbox, steer.DocsDir(inbox)
}

// notedID is the id an add printed: the last word of "noted <span> as <id>".
func notedID(t *testing.T, out string) string {
	t.Helper()
	f := strings.Fields(strings.SplitN(out, "\n", 2)[0])
	if len(f) < 4 || f[0] != "noted" {
		t.Fatalf("not an add's answer: %q", out)
	}
	return f[len(f)-1]
}

func runNote(t *testing.T, svc *domain.Service, inbox string, args ...string) (code int, out, errb string) {
	t.Helper()
	var o, e bytes.Buffer
	code = runSessionIn(inbox, svc, append([]string{"note"}, args...), nil, &o, &e)
	return code, o.String(), e.String()
}

func TestSessionNoteAddWritesAFileAndNamesIt(t *testing.T) {
	t.Parallel()
	svc, inbox, dir := docsCLI(t)
	code, out, errb := runNote(t, svc, inbox, "add", "a.go:1-2", "--summary", "look", "--rationale", "why", "--author", "claude", "--meta", "kind=design")
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb)
	}
	if !strings.HasPrefix(out, "noted a.go:1-2 as ") {
		t.Fatalf("stdout = %q", out)
	}
	id := notedID(t, out)
	if !agentdocs.ValidID(id) || strings.HasPrefix(id, "t") {
		t.Fatalf("id = %q, want a minted document id", id)
	}
	d, err := agentdocs.ReadDoc(dir, id, agentdocs.KindNote)
	if err != nil {
		t.Fatal(err)
	}
	n := d.Note
	if n.Path != "a.go" || n.Start != 1 || n.End != 2 || n.Summary != "look" || n.Rationale != "why" || n.Author != "claude" ||
		len(n.Lines) != 2 || n.Lines[0] != "package a" || n.Lines[1] != "" || len(n.Meta) != 1 || n.Meta[0].Key != "kind" {
		t.Fatalf("file = %+v", n)
	}
	if n.Created.IsZero() {
		t.Fatal("created missing")
	}
}

// The path is repo-relative, in slash form, whatever the shell's cwd; CRLF
// lines are stripped so a reader can place the note.
func TestSessionNoteAddFromASubdirectoryAndCRLF(t *testing.T) {
	t.Parallel()
	svc, inbox, dir := docsCLI(t)
	code, out, errb := runNote(t, svc, inbox, "add", "sub/b.go:2-3", "--summary", "s")
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb)
	}
	id := notedID(t, out)
	d, err := agentdocs.ReadDoc(dir, id, agentdocs.KindNote)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(d.Note.Lines, "|"); got != "two|three" {
		t.Fatalf("lines = %q", got)
	}
}

func TestSessionNoteAddRefusals(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	for _, tc := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"add", "a.go:9", "--summary", "s"}, 1, "line 9 is past the end of a.go (3 lines)"},
		{[]string{"add", "nope.go:1", "--summary", "s"}, 1, "nope.go has no lines to note"},
		{[]string{"add", "a.go:0", "--summary", "s"}, 2, "a line number is 1-based"},
		{[]string{"add", "a.go:3-2", "--summary", "s"}, 2, "the range ends before it starts"},
		{[]string{"add", "a.go:1"}, 2, "--summary is required"},
		{[]string{"add", "a.go:1", "--summary", strings.Repeat("x", 501)}, 1, "the summary is longer than 500 characters"},
		{[]string{"add", "f3:1", "--summary", "s"}, 1, "f3 has no lines to note"},
		{[]string{"add", "../x:1", "--summary", "s"}, 1, "../x has no lines to note"},
		{[]string{"add", "logo.png:1", "--summary", "s"}, 1, "logo.png has no lines to note"}, // binary (spec §6.2)
	} {
		code, _, errb := runNote(t, svc, inbox, tc.args...)
		if code != tc.code || !strings.Contains(errb, tc.msg) {
			t.Errorf("%v: exit %d stderr %q, want %d %q", tc.args, code, errb, tc.code, tc.msg)
		}
	}
}

func TestSessionNoteListShowRmClear(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	_, out1, _ := runNote(t, svc, inbox, "add", "a.go:3", "--summary", "gate")
	_, out2, _ := runNote(t, svc, inbox, "add", "a.go:1", "--summary", "pkg")
	id1 := notedID(t, out1)
	id2 := notedID(t, out2)
	_, out, _ := runNote(t, svc, inbox, "list")
	want := id2 + "\ta.go\t1-1\tpkg\n" + id1 + "\ta.go\t3-3\tgate\n" // by path, then start
	if out != want {
		t.Fatalf("list = %q, want %q", out, want)
	}
	_, out, _ = runNote(t, svc, inbox, "list", "a.go", "--json")
	var notes []steer.FileNote
	if err := json.Unmarshal([]byte(out), &notes); err != nil || len(notes) != 2 || notes[0].ID != id2 || notes[0].Created == "" {
		t.Fatalf("list --json = %s (%v)", out, err)
	}
	_, out, _ = runNote(t, svc, inbox, "show", id1)
	if !strings.HasPrefix(out, id1+"\ta.go:3-3\tagent\ngate\n\n3\tfunc gate() {}\n") {
		t.Fatalf("show = %q", out)
	}
	code, out, _ := runNote(t, svc, inbox, "rm", id1)
	if code != 0 || out != "removed "+id1+"\n" {
		t.Fatalf("rm: %d %q", code, out)
	}
	code, _, errb := runNote(t, svc, inbox, "rm", id1)
	if code != 1 || !strings.Contains(errb, "no note "+id1) {
		t.Fatalf("rm again: %d %q", code, errb)
	}
	code, out, _ = runNote(t, svc, inbox, "clear", "a.go")
	if code != 0 || out != "removed 1 notes on a.go\n" {
		t.Fatalf("clear: %d %q", code, out)
	}
	code, _, errb = runNote(t, svc, inbox, "clear", "a.go")
	if code != 1 || !strings.Contains(errb, "no notes on a.go") {
		t.Fatalf("clear again: %d %q", code, errb)
	}
	_, out, _ = runNote(t, svc, inbox, "list", "--json")
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("list after clear = %q", out)
	}
}

// A note whose lines moved is listed at its new place; one whose lines are
// gone says (outdated).
func TestSessionNoteListAlignsToTheFileNow(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	_, out, _ := runNote(t, svc, inbox, "add", "a.go:3", "--summary", "gate")
	if !strings.HasPrefix(out, "noted a.go:3 as ") { // one line prints no range
		t.Fatalf("stdout = %q", out)
	}
	id := notedID(t, out)
	top := callerWorktree(svc)
	if err := os.WriteFile(filepath.Join(top, "a.go"), []byte("// new\npackage a\n\nfunc gate() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, _ = runNote(t, svc, inbox, "list")
	if out != id+"\ta.go\t4-4\tgate\n" {
		t.Fatalf("moved: %q", out)
	}
	if err := os.WriteFile(filepath.Join(top, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, _ = runNote(t, svc, inbox, "list")
	if !strings.HasSuffix(out, "gate (outdated)\n") {
		t.Fatalf("outdated: %q", out)
	}
}

// No window needs to be live, and a live one is never asked: the verbs are
// file operations.
func TestSessionNoteNeedsNoWindowAndPostsNothing(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	livePresence(t, inbox)
	posted := make(chan steer.Command, 4)
	go func() { // a consumer that would see anything posted
		for i := 0; i < 30; i++ {
			for _, c := range steer.Drain(inbox) {
				posted <- c
			}
			time.Sleep(50 * time.Millisecond)
		}
		close(posted)
	}()
	if code, _, errb := runNote(t, svc, inbox, "add", "a.go:1", "--summary", "s"); code != 0 {
		t.Fatalf("exit = %d (%q)", code, errb)
	}
	for c := range posted {
		t.Fatalf("posted %+v", c)
	}
}
