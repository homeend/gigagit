package git

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"testing"
)

func TestArchiveFilesSubset(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	writeFile(t, dir, "keep.txt", "keep me\n")
	writeFile(t, dir, "drop.txt", "ignore me\n")
	commitAll(t, dir, "two files")
	head := gitOut(t, dir, "rev-parse", "HEAD")

	data, err := repo.ArchiveFiles(context.Background(), head, []string{"keep.txt"})
	if err != nil {
		t.Fatalf("ArchiveFiles: %v", err)
	}
	names := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		b, _ := io.ReadAll(tr)
		names[h.Name] = string(b)
	}
	if names["keep.txt"] != "keep me\n" {
		t.Fatalf("keep.txt = %q, want %q", names["keep.txt"], "keep me\n")
	}
	if _, ok := names["drop.txt"]; ok {
		t.Fatal("drop.txt should not be in a keep.txt-only archive")
	}
}

func TestArchiveFilesEmptyPathsErrors(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if _, err := repo.ArchiveFiles(context.Background(), "HEAD", nil); err == nil {
		t.Fatal("empty paths must error, not archive the whole tree")
	}
}
