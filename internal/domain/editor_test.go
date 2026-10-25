package domain

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
)

func TestEditorArgv(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		editor string
		line   int
		want   []string
	}{
		{"vi", 12, []string{"vi", "+12", "/wt/a.go"}},
		{"/usr/bin/nvim -u NONE", 3, []string{"/usr/bin/nvim", "-u", "NONE", "+3", "/wt/a.go"}},
		{"code -w", 12, []string{"code", "-w", "--goto", "/wt/a.go:12"}},
		{`C:\bin\Code.exe`, 2, []string{`C:\bin\Code.exe`, "--goto", "/wt/a.go:2"}},
		{"hx", 7, []string{"hx", "/wt/a.go:7"}},
		{"gedit", 7, []string{"gedit", "/wt/a.go"}}, // unknown: the line is dropped, never guessed
		{"vi", 0, []string{"vi", "/wt/a.go"}},
		{"code -w", 0, []string{"code", "-w", "/wt/a.go"}},
	} {
		if got := EditorArgv(c.editor, "/wt/a.go", c.line); !reflect.DeepEqual(got, c.want) {
			t.Errorf("EditorArgv(%q, %d) = %q, want %q", c.editor, c.line, got, c.want)
		}
	}
	// An empty editor falls back to the default, which still gets its goto.
	if got := EditorArgv("  ", "/x", 0); len(got) != 2 || got[0] != DefaultEditor() || got[1] != "/x" {
		t.Errorf("empty editor = %q", got)
	}
}

func TestResolveEditor(t *testing.T) {
	t.Parallel()
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, c := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"VISUAL": "myvis", "EDITOR": "myed"}, "myvis"},
		{map[string]string{"VISUAL": "  ", "EDITOR": "myed"}, "myed"}, // whitespace is unset
		{map[string]string{"EDITOR": " "}, DefaultEditor()},
		{map[string]string{}, DefaultEditor()},
	} {
		if got := ResolveEditor(env(c.env)); got != c.want {
			t.Errorf("ResolveEditor(%v) = %q, want %q", c.env, got, c.want)
		}
	}
}

func TestViewCopy(t *testing.T) {
	t.Parallel()
	path, err := WriteViewCopy("pkg/sub/foo.go", []byte("package x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(path), "gg-") || !strings.HasSuffix(path, "-foo.go") {
		t.Errorf("name %q: want gg-*-foo.go", path)
	}
	if b, _ := os.ReadFile(path); string(b) != "package x\n" {
		t.Errorf("bytes = %q", b)
	}
	if fi, err := os.Stat(path); err == nil && runtime.GOOS != "windows" && fi.Mode().Perm() != 0o400 {
		t.Errorf("mode = %v, want 0400", fi.Mode().Perm())
	}
	RemoveViewCopy(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("RemoveViewCopy left %s", path)
	}
}

func TestWorktreeAbsPath(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	real, _ := filepath.EvalSymlinks(dir)
	got, err := svc.WorktreeAbsPath(context.Background(), "sub/a.txt")
	if err != nil || filepath.Clean(got) != filepath.Join(real, "sub", "a.txt") {
		t.Errorf("WorktreeAbsPath = %q, %v", got, err)
	}
	for _, bad := range []string{"../x", "sub/../../x"} {
		if _, err := svc.WorktreeAbsPath(context.Background(), bad); err == nil {
			t.Errorf("WorktreeAbsPath(%q): want refused", bad)
		}
	}
}

// An editor session is a Terminal; when the editor exits its done runs and
// the session leaves the list.
func TestStartEditorCleansUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := UseSessionManager(agentsession.NewManager())
	defer restore()
	defer Sessions().KillAll(context.Background())
	dir := cleanDir(t)
	ran := make(chan struct{})
	s, err := Open(dir).StartEditor(context.Background(), []string{"/bin/sh", "-c", "sleep 0.2"}, "Editor: a.go", dir, "", 80, 10, func() { close(ran) })
	if err != nil {
		t.Fatal(err)
	}
	if info := s.Info(); info.Label != "Editor: a.go" || !info.Terminal {
		t.Fatalf("info = %+v", info)
	}
	select {
	case <-ran:
	case <-time.After(10 * time.Second):
		t.Fatal("done never ran")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := Sessions().Get(s.Info().ID); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the editor session is still listed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
