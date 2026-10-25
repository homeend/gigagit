package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type editorStartCall struct {
	argv  []string
	label string
	dir   string
	done  func()
}

// editorServer: a repo with a commit, a staged file and a dirty one; the
// editor start is captured instead of run.
func editorServer(t *testing.T) (*Server, string, *[]editorStartCall) {
	t.Helper()
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "fakeed -w")
	srv, root := lifecycleServer(t, "")
	if err := os.WriteFile(filepath.Join(root, "s.txt"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "s.txt")
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := &[]editorStartCall{}
	srv.editorStart = func(_ *domain.Service, argv []string, label, dir, _ string, _, _ int, done func()) (string, error) {
		*calls = append(*calls, editorStartCall{argv, label, dir, done})
		return fmt.Sprintf("ed-%d", len(*calls)), nil
	}
	return srv, root, calls
}

func TestEditorOpenLiveEdit(t *testing.T) {
	srv, root, calls := editorServer(t)
	ts := serve(t, srv)
	code, body := postJSONAny(t, ts, "/api/editor-open", `{"source":"worktree","path":"f.txt","line":3}`)
	if code != http.StatusOK || len(*calls) != 1 {
		t.Fatalf("code = %d body = %v calls = %d", code, body, len(*calls))
	}
	c := (*calls)[0]
	real, _ := filepath.EvalSymlinks(root)
	want := []string{"fakeed", "-w", filepath.Join(real, "f.txt")} // an unknown editor: no line flag
	if strings.Join(c.argv, " ") != strings.Join(want, " ") {
		t.Errorf("argv = %q, want %q", c.argv, want)
	}
	if c.label != "Editor: f.txt" || body["id"] != "ed-1" || body["name"] != "f.txt" || body["live"] != true {
		t.Errorf("label %q, body %v", c.label, body)
	}
}

func TestEditorOpenLineFlag(t *testing.T) {
	srv, _, calls := editorServer(t)
	t.Setenv("EDITOR", "vim")
	ts := serve(t, srv)
	if code, body := postJSONAny(t, ts, "/api/editor-open", `{"source":"worktree","path":"f.txt","line":7}`); code != http.StatusOK {
		t.Fatalf("code = %d %v", code, body)
	}
	if a := (*calls)[0].argv; len(a) != 3 || a[0] != "vim" || a[1] != "+7" {
		t.Errorf("argv = %q", a)
	}
}

// A view hands the editor a read-only copy holding the source's bytes; its
// done removes the copy.
func TestEditorOpenViews(t *testing.T) {
	srv, root, calls := editorServer(t)
	head := strings.TrimSpace(gitOut(t, root, "rev-parse", "HEAD"))
	ts := serve(t, srv)
	for _, c := range []struct {
		body, name, want string
	}{
		{`{"source":"staged","path":"s.txt"}`, "s.txt", "staged\n"},
		{`{"source":"commit","path":"f.txt","rev":"` + head + `"}`, "f.txt", "content 1\n"},
		{`{"source":"message","rev":"` + head + `"}`, "COMMIT_EDITMSG", "c1"},
		{`{"source":"worktree-copy","path":"f.txt"}`, "f.txt", "dirty\n"},
	} {
		n := len(*calls)
		code, body := postJSONAny(t, ts, "/api/editor-open", c.body)
		if code != http.StatusOK || len(*calls) != n+1 {
			t.Fatalf("%s: code = %d %v", c.body, code, body)
		}
		call := (*calls)[n]
		copyPath := call.argv[len(call.argv)-1]
		data, err := os.ReadFile(copyPath)
		if err != nil || !strings.HasPrefix(string(data), c.want) || !strings.HasSuffix(copyPath, "-"+c.name) {
			t.Errorf("%s: copy %s = %q, %v", c.body, copyPath, data, err)
		}
		if body["live"] != false || body["name"] != c.name {
			t.Errorf("%s: body %v", c.body, body)
		}
		call.done()
		if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
			t.Errorf("%s: the copy outlived the editor", c.body)
		}
	}
}

func TestEditorOpenRefusals(t *testing.T) {
	srv, _, calls := editorServer(t)
	ts := serve(t, srv)
	for _, c := range []struct {
		body string
		code int
	}{
		{`{"source":"worktree","path":"../x"}`, http.StatusBadRequest},
		{`{"source":"worktree","path":"/etc/passwd"}`, http.StatusBadRequest},
		{`{"source":"worktree","path":""}`, http.StatusBadRequest},
		{`{"source":"commit","path":"f.txt","rev":"--output=x"}`, http.StatusBadRequest},
		{`{"source":"commit","path":"f.txt","rev":"0123456789abcdef0123456789abcdef01234567"}`, http.StatusNotFound},
		{`{"source":"message","rev":"HEAD~1"}`, http.StatusBadRequest},
		{`{"source":"bookmark","id":"nope"}`, http.StatusNotFound},
		{`{"source":"shelf","id":"nope"}`, http.StatusNotFound},
		{`{"source":"nope"}`, http.StatusBadRequest},
	} {
		if code, body := postJSONAny(t, ts, "/api/editor-open", c.body); code != c.code {
			t.Errorf("%s: code = %d, want %d (%v)", c.body, code, c.code, body)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("a refused open started an editor: %v", *calls)
	}
}

// The page's editor model: the diff line (the clicked row, else the first
// one in view), what a diff opens (live on a working-tree diff, a view of a
// commit's file, nothing on a compare), and which editors have finished.
func TestEditorJS(t *testing.T) {
	t.Parallel()
	var mod strings.Builder
	for _, fn := range []string{"pickEditLine", "diffEditReq", "finishedEditors"} {
		mod.WriteString(jsFunc(t, "editor.js", fn) + "\n")
	}
	src := mod.String() + `
const sha = "0123456789abcdef0123456789abcdef01234567";
console.log(JSON.stringify([
  pickEditLine([{rno: 3, visible: false}, {rno: 0, visible: true}, {rno: 9, visible: true}, {rno: 12, visible: true, cur: true}]),
  pickEditLine([{rno: 3, visible: false}, {rno: 0, visible: true}, {rno: 9, visible: true}]),
  pickEditLine([{rno: 0, visible: true, cur: true}, {rno: 5, visible: true}]), // a deleted line under the cursor: the next in view
  pickEditLine([]),
  diffEditReq({path: "a.go", rev: "", state: "unstaged"}, 4),
  diffEditReq({path: "a.go", rev: "", state: "staged"}, 4),
  diffEditReq({path: "a.go", rev: sha, state: "commit"}, 7),
  diffEditReq({path: "a.go", rev: sha, state: "commit", compare: true}, 7),
  diffEditReq({path: "a.go", rev: sha, compare: true, cmpSides: {left: "commit:" + sha, right: "worktree"}}, 7),
  diffEditReq({path: "a.go", rev: "main", state: "commit"}, 7),
  diffEditReq(null, 1),
  finishedEditors(new Map([["e1", {}], ["e2", {}]]), [{id: "e2"}, {id: "x"}]),
]));
`
	want := `[12,9,5,0,{"source":"worktree","path":"a.go","line":4},{"source":"worktree","path":"a.go","line":4},` +
		`{"source":"commit","path":"a.go","rev":"0123456789abcdef0123456789abcdef01234567","line":7},null,null,null,null,["e1"]]`
	if got := nodeRun(t, src); got != want {
		t.Errorf("editor model =\n%s\nwant\n%s", got, want)
	}
}

func TestEditorWired(t *testing.T) {
	t.Parallel()
	keys := readStaticSrc(t, "keys.js")
	live := readStaticSrc(t, "live.js")
	con := readStaticSrc(t, "console.js")
	files := readStaticSrc(t, "files.js")
	hist := readStaticSrc(t, "filehist.js")
	html := readStaticSrc(t, "index.html")
	for _, pin := range []struct{ src, want, why string }{
		{keys, `e.key === "I" && !e.ctrlKey && !e.metaKey && !e.altKey && state.pane === "commits"`, "I on the commit list opens its message"},
		{keys, `e.key === "e" && !e.ctrlKey && !e.metaKey && !e.altKey && diffEditOpen()`, "e in a diff opens the editor at the line"},
		{keys, `case "editor": diffEditOpen(); break;`, "the foot chip runs e"},
		{html, `<button data-act="editor">e editor</button>`, "the foot advertises e"},
		{live, "editorsChanged(msg.sessions || [])", "a finished editor is noticed on the sessions event"},
		{con, "if (opts.editor) return opts.editor();", "an editor gone before its console opens is no error"},
		{files, "const edit = diffEditRow(row);", "the diff menu offers open in editor at line N"},
		{hist, `if (e.key === "e" && !e.ctrlKey && !e.metaKey && !e.altKey) {`, "history and blame take e"},
		{hist, `source: "worktree-copy"`, "a working-tree blame opens the on-disk file as a copy"},
		{html, "e editor · esc closes</div>", "the history and blame hints advertise e"},
	} {
		if !strings.Contains(pin.src, pin.want) {
			t.Errorf("%s: missing %q", pin.why, pin.want)
		}
	}
	if n := strings.Count(hist, `if (e.key === "e" && !e.ctrlKey && !e.metaKey && !e.altKey) {`); n != 2 {
		t.Errorf("history and blame each take e: %d", n)
	}
	if n := strings.Count(html, "e editor · esc closes</div>"); n != 2 {
		t.Errorf("both hints advertise e: %d", n)
	}
}

// Review fixes: the line comes from the right number cell every layout draws,
// inside the anchored file only; the live edit, the staged copy and the diff
// wait for a running operation; the F finder edits too.
func TestEditorReviewFixesWired(t *testing.T) {
	t.Parallel()
	ed := readStaticSrc(t, "editor.js")
	fin := readStaticSrc(t, "wtfinder.js")
	for _, pin := range []struct{ src, want, why string }{
		{ed, `tr.querySelector("td.no.r")`, "the new-side line is the right number cell"},
		{ed, "const scope = noteScope();", "a stack reads the anchored file's section only"},
		{ed, `scope.querySelectorAll("table.diff tr[data-i]")`, "every drawn row counts, not only data-rno ones"},
		{ed, "if (gate && opBusy()) return;", "the TUI's opsIdle gate"},
		{ed, `openInEditor({ source: "worktree", path: ctx.path }, true)`, "the live edit is gated"},
		{ed, `openInEditor({ source: "staged", path: ctx.path }, true)`, "the staged copy is gated"},
		{ed, "openInEditor(req, true);", "the diff's e is gated"},
		{fin, `rows.push({ label: "edit in editor", act: () => openInEditor({ source: "worktree", path: f.path }, true) });`, "the F finder's Edit in editor"},
	} {
		if !strings.Contains(pin.src, pin.want) {
			t.Errorf("%s: missing %q", pin.why, pin.want)
		}
	}
	if strings.Contains(ed, "tr[data-rno]") {
		t.Errorf("editor.js still reads data-rno (split layout with notes only)")
	}
}
