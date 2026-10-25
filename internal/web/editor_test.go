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
