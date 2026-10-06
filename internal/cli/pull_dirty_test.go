package cli

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cloneBehindDirty: the clone is behind and holds an edit to the file the
// pull changes, so git refuses the pull.
func cloneBehindDirty(t *testing.T) string {
	t.Helper()
	clone := cloneBehind(t)
	os.WriteFile(filepath.Join(clone, "f.txt"), []byte("local\n"), 0o644)
	return clone
}

func TestPullOnDirtyDiscard(t *testing.T) {
	t.Parallel()
	clone := cloneBehindDirty(t)
	code, out, errb := runCLI(t, clone, "pull", "--on-dirty", "discard")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "changes discarded") {
		t.Fatalf("output = %q", out)
	}
	if b, _ := os.ReadFile(filepath.Join(clone, "f.txt")); string(b) != "v2\n" {
		t.Fatalf("f.txt = %q, want v2", b)
	}
}

// Shelve really stores the work before the clean: the entry holds the edit
// AND an untracked file (its content is the clone's path, so the entry found
// is this test's own in the shared state dir).
func TestPullOnDirtyShelve(t *testing.T) {
	t.Parallel()
	clone := cloneBehindDirty(t)
	os.WriteFile(filepath.Join(clone, "other.txt"), []byte(clone), 0o644)
	code, out, errb := runCLI(t, clone, "pull", "--on-dirty", "shelve")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "shelved as") {
		t.Fatalf("output = %q", out)
	}
	if b, _ := os.ReadFile(filepath.Join(clone, "f.txt")); string(b) != "v2\n" {
		t.Fatalf("f.txt = %q, want v2", b)
	}
	if _, err := os.Stat(filepath.Join(clone, "other.txt")); !os.IsNotExist(err) {
		t.Fatalf("other.txt must be cleaned after the shelve: %v", err)
	}
	svc := openCLIService(t, clone)
	es, err := svc.ShelfList(context.Background(), "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		blob, err := svc.ShelfBlob(context.Background(), e.ID)
		if err != nil {
			continue
		}
		members := map[string]string{}
		tr := tar.NewReader(bytes.NewReader(blob))
		for {
			h, err := tr.Next()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(tr)
			members[h.Name] = string(b)
		}
		if members["other.txt"] != clone {
			continue
		}
		if members["f.txt"] != "local\n" {
			t.Fatalf("shelved f.txt = %q, want the local edit", members["f.txt"])
		}
		return
	}
	t.Fatal("no shelf entry holds this clone's changes")
}

func TestPullOnDirtyAbortKeepsWork(t *testing.T) {
	t.Parallel()
	clone := cloneBehindDirty(t)
	code, out, errb := runCLI(t, clone, "pull", "--on-dirty", "abort")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "pull cancelled") {
		t.Fatalf("output = %q", out)
	}
	if b, _ := os.ReadFile(filepath.Join(clone, "f.txt")); string(b) != "local\n" {
		t.Fatalf("f.txt = %q, the edit must survive", b)
	}
}

// Without the flag a non-interactive run cannot answer: it fails naming the
// decision, touching nothing.
func TestPullDirtyWithoutFlagNamesDecision(t *testing.T) {
	t.Parallel()
	clone := cloneBehindDirty(t)
	code, _, errb := runCLI(t, clone, "pull")
	if code == 0 || !strings.Contains(errb, "pull.dirty") {
		t.Fatalf("exit = %d, stderr = %q; want a failure naming pull.dirty", code, errb)
	}
	if b, _ := os.ReadFile(filepath.Join(clone, "f.txt")); string(b) != "local\n" {
		t.Fatalf("f.txt = %q, the edit must survive", b)
	}
}

func TestPullOnDirtyBadValue(t *testing.T) {
	t.Parallel()
	code, _, errb := runCLI(t, cloneBehind(t), "pull", "--on-dirty", "commit")
	if code != 2 || !strings.Contains(errb, "--on-dirty") {
		t.Fatalf("exit = %d, stderr = %q; want 2 naming --on-dirty", code, errb)
	}
}
