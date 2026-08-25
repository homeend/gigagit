package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExportFileWritesNestedPath(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "out", "a1b2c3d.patch")
	op := ExportFile{Path: path, Data: []byte("From abc\n")}
	res, err := op.Run(context.Background(), OpDeps{}) // absent → no decider needed
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Changed {
		t.Fatal("expected Changed")
	}
	if got, _ := os.ReadFile(path); string(got) != "From abc\n" {
		t.Fatalf("content = %q", got)
	}
}

func TestExportFileIdenticalBytesNoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "x.patch")
	if err := os.WriteFile(path, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	op := ExportFile{Path: path, Data: []byte("same")}
	res, err := op.Run(context.Background(), OpDeps{}) // identical → no decision asked
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Changed {
		t.Fatal("identical bytes must be a no-op (Changed=false)")
	}
}

func TestExportFileExistingCancels(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "x.patch")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	op := ExportFile{Path: path, Data: []byte("new")}
	_, err := op.Run(context.Background(), OpDeps{Decider: MapDecider{"overwrite": "cancel"}})
	if err != ErrWriteCancelled {
		t.Fatalf("err = %v, want ErrWriteCancelled", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Fatalf("cancel must leave the file untouched, got %q", got)
	}
}

func TestExportFileExistingOverwrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "x.patch")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	op := ExportFile{Path: path, Data: []byte("new")}
	res, err := op.Run(context.Background(), OpDeps{Decider: MapDecider{"overwrite": "overwrite"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Changed {
		t.Fatal("expected Changed after overwrite")
	}
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Fatalf("content = %q, want new", got)
	}
}
