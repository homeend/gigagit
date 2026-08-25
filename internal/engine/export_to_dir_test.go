package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestExportToDirWritesNestedFiles(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "commit-abc1234")
	op := ExportToDir{
		Dir: dir,
		Files: []model.ExportFile{
			{RelPath: "src/a.txt", Data: []byte("alpha\n")},
			{RelPath: "b.txt", Data: []byte("bee\n")},
		},
	}
	res, err := op.Run(context.Background(), OpDeps{}) // no decider: dir is absent
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Changed {
		t.Fatal("expected Changed")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "src", "a.txt")); string(got) != "alpha\n" {
		t.Fatalf("src/a.txt = %q, want alpha\\n", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(got) != "bee\n" {
		t.Fatalf("b.txt = %q", got)
	}
}

func TestExportToDirExistingDirCancels(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // already exists
	op := ExportToDir{Dir: dir, Files: []model.ExportFile{{RelPath: "x", Data: []byte("y")}}}
	if _, err := op.Run(context.Background(), OpDeps{Decider: MapDecider{"overwrite": "cancel"}}); err != ErrExportCancelled {
		t.Fatalf("err = %v, want ErrExportCancelled", err)
	}
}
