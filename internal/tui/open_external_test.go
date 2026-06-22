package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReadOnlyTempFilePreservesExtension(t *testing.T) {
	path, err := writeReadOnlyTempFile("pkg/sub/foo.go", []byte("package x\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer removeTempFile(path)

	if !strings.HasSuffix(path, ".go") {
		t.Errorf("temp name must end in the real extension (for syntax highlighting), got %q", path)
	}
	if base := filepath.Base(path); !strings.HasSuffix(base, "foo.go") {
		t.Errorf("temp base should end in the real file name, got %q", base)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "package x\n" {
		t.Fatalf("temp content = %q, %v", data, err)
	}
	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o222 != 0 {
		t.Errorf("temp file should be read-only (no write bits), mode = %v", info.Mode().Perm())
	}
}

// removeTempFile must delete even a 0400 file (the read-only bit is cleared
// first), which is what lets the Windows path succeed too.
func TestRemoveTempFileDeletesReadOnly(t *testing.T) {
	path, err := writeReadOnlyTempFile("foo.txt", []byte("hi"))
	if err != nil {
		t.Fatal(err)
	}
	removeTempFile(path)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp file should be gone, stat err = %v", err)
	}
}

func TestOpenInEditorCmdWritesResolvedBytes(t *testing.T) {
	m := Model{}
	cmd := m.openInEditorCmd("a/b/main.go", func(context.Context) ([]byte, error) {
		return []byte("func main(){}\n"), nil
	})
	msg, ok := cmd().(editorViewMsg)
	if !ok {
		t.Fatalf("want editorViewMsg, got %T", cmd())
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	defer removeTempFile(msg.path)
	if msg.name != "a/b/main.go" {
		t.Errorf("name = %q, want the real path", msg.name)
	}
	data, _ := os.ReadFile(msg.path)
	if string(data) != "func main(){}\n" {
		t.Errorf("temp content = %q", data)
	}
}

func TestOpenInEditorCmdSurfacesResolveError(t *testing.T) {
	m := Model{}
	cmd := m.openInEditorCmd("x.go", func(context.Context) ([]byte, error) {
		return nil, errors.New("boom")
	})
	msg := cmd().(editorViewMsg)
	if msg.err == nil || msg.path != "" {
		t.Fatalf("resolve error must surface with no temp file: %+v", msg)
	}
}

// The view-finish handler removes the temp file and shows a "viewed <real
// name>" notice (the REAL name, not the temp path) — and must NOT trigger a
// status reload.
func TestEditorViewFinishedCleansUpAndNotices(t *testing.T) {
	path, err := writeReadOnlyTempFile("foo.go", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	m := Model{}
	updated, cmd := m.Update(editorViewFinishedMsg{path: path, name: "pkg/foo.go"})
	mm := updated.(Model)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("finish must delete the temp file, stat err = %v", err)
	}
	if mm.statusMsg != "viewed foo.go" {
		t.Errorf("status = %q, want %q", mm.statusMsg, "viewed foo.go")
	}
	if cmd != nil {
		t.Error("viewing changes nothing; the finish handler must not dispatch a status reload")
	}
}
