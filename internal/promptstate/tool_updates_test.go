package promptstate

import (
	"path/filepath"
	"testing"
)

func TestDeclineToolUpdatePersists(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "prompts.toml")
	fs := NewFileStore(path)
	if err := fs.DeclineToolUpdate("review\x00A\x00sha256:1"); err != nil {
		t.Fatal(err)
	}
	if err := fs.DeclineToolUpdate("review\x00A\x00sha256:1"); err != nil { // idempotent
		t.Fatal(err)
	}
	got := NewFileStore(path).DeclinedToolUpdates()
	if len(got) != 1 || !got[ToolUpdateID("review\x00A\x00sha256:1")] {
		t.Fatalf("got %v", got)
	}
	if got[ToolUpdateID("review\x00A\x00sha256:2")] {
		t.Fatal("a different offer must not read as declined")
	}
}
