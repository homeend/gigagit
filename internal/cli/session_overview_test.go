package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// runOverview runs `gg session overview …` against inbox with stdin piped
// (nil without svc: a routing test that never reaches the directory).
func runOverview(t *testing.T, svc *domain.Service, inbox, stdin string, args ...string) (code int, out, errb string) {
	t.Helper()
	var o, e bytes.Buffer
	var in io.Reader
	if stdin != "" {
		in = strings.NewReader(stdin)
	}
	code = runSessionIn(inbox, svc, append([]string{"overview"}, args...), in, &o, &e)
	return code, o.String(), e.String()
}

// add writes <id>.md with the front matter gg's encoder spells, prints the
// id, the unresolved anchors and the backtick/no-anchor warnings; with no
// window live it prints nothing more and exits 0.
func TestSessionOverviewAddWritesAFile(t *testing.T) {
	t.Parallel()
	svc, inbox, dir := docsCLI(t)
	code, out, errb := runOverview(t, svc, inbox, "See [gate](a.go:3) and [gone](nope.go) and `[x](a.go:1)`.\n", "add", "--title", "Tour")
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	id := lines[0]
	if !agentdocs.ValidID(id) || strings.HasPrefix(id, "f") {
		t.Fatalf("first line = %q, want a minted id", id)
	}
	if len(lines) != 3 || lines[1] != "unresolved: nope.go" || !strings.HasPrefix(lines[2], "in backticks: ") {
		t.Fatalf("stdout = %q", out)
	}
	if errb != "" {
		t.Fatalf("stderr = %q", errb)
	}
	d, err := agentdocs.ReadDoc(dir, id, agentdocs.KindOverview)
	if err != nil {
		t.Fatal(err)
	}
	if d.Over.Title != "Tour" || d.Over.Author != "agent" || d.Over.Created.IsZero() || !strings.Contains(d.Over.Text, "[gate](a.go:3)") {
		t.Fatalf("file = %+v", d.Over)
	}
}

// With a window live, add ends by asking it to show the id (file_focus) and
// prints the reply's detail; --background writes the same file and posts
// nothing.
func TestSessionOverviewAddFocusesTheLiveWindowUnlessBackground(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	livePresence(t, inbox)
	seen := answer(t, inbox, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: true, Detail: "focused overview " + c.FileID + " \"T\""}
	})
	code, out, errb := runOverview(t, svc, inbox, "[a](a.go:1)\n", "add", "--title", "T")
	if code != 0 {
		t.Fatalf("exit = %d (%q)", code, errb)
	}
	c := <-seen
	id := strings.Split(out, "\n")[0]
	if c.Cmd != "file_focus" || c.FileID != id || !c.Wait || c.Worktree != callerWorktree(svc) {
		t.Fatalf("posted %+v, want the caller's worktree %s", c, callerWorktree(svc))
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "focused overview "+id+" \"T\"") {
		t.Fatalf("stdout = %q", out)
	}
	posted := make(chan steer.Command, 4)
	go func() {
		for i := 0; i < 30; i++ {
			for _, c := range steer.Drain(inbox) {
				posted <- c
			}
			time.Sleep(50 * time.Millisecond)
		}
		close(posted)
	}()
	code, out, _ = runOverview(t, svc, inbox, "[a](a.go:1)\n", "add", "--title", "T", "--background")
	if code != 0 || len(strings.Split(strings.TrimSpace(out), "\n")) != 1 {
		t.Fatalf("background: %d %q", code, out)
	}
	for c := range posted {
		t.Fatalf("background posted %+v", c)
	}
}

func TestSessionOverviewSetListShowRm(t *testing.T) {
	t.Parallel()
	svc, inbox, dir := docsCLI(t)
	_, out, _ := runOverview(t, svc, inbox, "[a](a.go:1)\n", "add", "--title", "First", "--background")
	id1 := strings.TrimSpace(out)
	time.Sleep(1100 * time.Millisecond) // minted ids carry the second: the second overview is newer
	_, out, _ = runOverview(t, svc, inbox, "[a](a.go:1) [b](a.go:2)\n", "add", "--title", "Second", "--background")
	id2 := strings.TrimSpace(out)
	code, out, _ := runOverview(t, svc, inbox, "[c](a.go:3)\n", "set", id1, "--background")
	if code != 0 || strings.TrimSpace(out) != id1 {
		t.Fatalf("set: %d %q", code, out)
	}
	d, _ := agentdocs.ReadDoc(dir, id1, agentdocs.KindOverview)
	if d.Over.Title != "First" || !strings.Contains(d.Over.Text, "[c](a.go:3)") {
		t.Fatalf("set kept the title and replaced the text: %+v", d.Over)
	}
	code, _, errb := runOverview(t, svc, inbox, "x\n", "set", "20260101T000000-0000", "--background")
	if code != 1 || !strings.Contains(errb, "no overview 20260101T000000-0000") {
		t.Fatalf("set missing: %d %q", code, errb)
	}
	_, out, _ = runOverview(t, svc, inbox, "", "list")
	if out != id2+"\t2 anchors\tSecond\n"+id1+"\t1 anchors\tFirst\n" { // newest first
		t.Fatalf("list = %q", out)
	}
	_, out, _ = runOverview(t, svc, inbox, "", "list", "--json")
	var ovs []steer.Overview
	if err := json.Unmarshal([]byte(out), &ovs); err != nil || len(ovs) != 2 || ovs[0].Author != "agent" || ovs[0].Created == "" || ovs[0].Anchors != 2 {
		t.Fatalf("list --json = %s (%v)", out, err)
	}
	_, out, _ = runOverview(t, svc, inbox, "", "show", id1)
	if !strings.HasPrefix(out, id1+"\tFirst\tagent\t") || !strings.HasSuffix(out, "\n\n[c](a.go:3)\n") {
		t.Fatalf("show = %q", out)
	}
	code, out, _ = runOverview(t, svc, inbox, "", "rm", id1)
	if code != 0 || out != "removed "+id1+"\n" {
		t.Fatalf("rm: %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, id1+".md")); !os.IsNotExist(err) {
		t.Fatal("rm left the file")
	}
	code, _, errb = runOverview(t, svc, inbox, "", "show", id1)
	if code != 1 || !strings.Contains(errb, "no overview "+id1) {
		t.Fatalf("show after rm: %d %q", code, errb)
	}
}

func TestSessionOverviewMisuse(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	for _, tc := range []struct {
		stdin string
		args  []string
		msg   string
	}{
		{"x\n", []string{"add"}, "--title is required"},
		{"", []string{"add", "--title", "T"}, "give the text with --file or on stdin"},
		{"\n", []string{"add", "--title", "T"}, "the text is empty"},
		{"x\n", []string{"add", "--title", strings.Repeat("t", 201)}, "the title must be one line"},
		{"x\n", []string{"set", "a/b"}, "usage: gg session overview"},
		{"", []string{"show", "a.md"}, "usage: gg session overview"},
	} {
		code, _, errb := runOverview(t, svc, inbox, tc.stdin, tc.args...)
		if code != 2 || !strings.Contains(errb, tc.msg) {
			t.Errorf("%v: exit %d stderr %q, want 2 %q", tc.args, code, errb, tc.msg)
		}
	}
}

// export reads the overview and the notes from the directory (no window).
func TestSessionOverviewExportReadsTheDirectory(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	_, out, _ := runNote(t, svc, inbox, "add", "a.go:3", "--summary", "gate")
	nid := notedID(t, out)
	_, out, _ = runOverview(t, svc, inbox, "[why](note:"+nid+")\n", "add", "--title", "Tour", "--background")
	id := strings.TrimSpace(out)
	code, out, errb := runOverview(t, svc, inbox, "", "export", id, "--out", "-")
	if code != 0 {
		t.Fatalf("exit = %d (%q)", code, errb)
	}
	if !strings.Contains(out, "<html") || !strings.Contains(out, "Tour") || !strings.Contains(out, "func gate() {}") {
		t.Fatalf("export = %.200q…", out)
	}
	code, _, errb = runOverview(t, svc, inbox, "", "export", "bogus", "--out", "-")
	if code != 1 || !strings.Contains(errb, "no overview bogus") {
		t.Fatalf("export of nothing: %d %q", code, errb)
	}
}

func TestSessionDocsPathAndLs(t *testing.T) {
	t.Parallel()
	svc, inbox, dir := docsCLI(t)
	var o, e bytes.Buffer
	if code := runSessionIn(inbox, svc, []string{"docs", "path"}, nil, &o, &e); code != 0 || strings.TrimSpace(o.String()) != dir {
		t.Fatalf("path: %d %q %q", code, o.String(), e.String())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("docs path created the directory")
	}
	o.Reset()
	if code := runSessionIn(inbox, svc, []string{"docs", "ls"}, nil, &o, &e); code != 0 || o.String() != "" {
		t.Fatalf("ls of nothing: %d %q", code, o.String())
	}
	_, out, _ := runNote(t, svc, inbox, "add", "a.go:1", "--summary", "pkg")
	nid := notedID(t, out)
	_, out, _ = runOverview(t, svc, inbox, "[a](a.go:1)\n", "add", "--title", "T", "--background")
	oid := strings.TrimSpace(out)
	o.Reset()
	if code := runSessionIn(inbox, svc, []string{"docs", "ls"}, nil, &o, &e); code != 0 {
		t.Fatalf("ls: %d %q", code, e.String())
	}
	want := "overview\t" + oid + "\t1 anchors\tT\nnote\t" + nid + "\ta.go\t1-1\tpkg\n"
	if o.String() != want {
		t.Fatalf("ls = %q, want %q", o.String(), want)
	}
	o.Reset()
	if code := runSessionIn(inbox, svc, []string{"docs", "ls", "--json"}, nil, &o, &e); code != 0 {
		t.Fatalf("ls --json: %d", code)
	}
	var got struct {
		Dir       string           `json:"dir"`
		Overviews []steer.Overview `json:"overviews"`
		Notes     []steer.FileNote `json:"notes"`
	}
	if err := json.Unmarshal(o.Bytes(), &got); err != nil || got.Dir != dir || len(got.Overviews) != 1 || len(got.Notes) != 1 {
		t.Fatalf("ls --json = %s (%v)", o.String(), err)
	}
}
