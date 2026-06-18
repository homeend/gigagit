package engine

import (
	"context"
	"errors"
	"testing"
)

// writeFakeRepo implements only the worktree read/write verbs WriteFile needs.
type writeFakeRepo struct {
	GitOps   // nil embed: only Read/WriteWorktreeFile implemented
	existing map[string][]byte
	written  map[string][]byte
}

func newWriteFake() *writeFakeRepo {
	return &writeFakeRepo{existing: map[string][]byte{}, written: map[string][]byte{}}
}

func (f *writeFakeRepo) ReadWorktreeFile(_ context.Context, path string) ([]byte, error) {
	if b, ok := f.existing[path]; ok {
		return b, nil
	}
	return nil, errors.New("not found")
}

func (f *writeFakeRepo) WriteWorktreeFile(_ context.Context, path string, content []byte) error {
	f.written[path] = content
	f.existing[path] = content
	return nil
}

func TestWriteFileWritesWhenDestAbsent(t *testing.T) {
	repo := newWriteFake()
	_, err := WriteFile{Path: "x.txt", Data: []byte("new")}.Run(context.Background(),
		OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if string(repo.written["x.txt"]) != "new" {
		t.Fatalf("written = %q", repo.written["x.txt"])
	}
}

func TestWriteFileNoopWhenIdentical(t *testing.T) {
	repo := newWriteFake()
	repo.existing["x.txt"] = []byte("same")
	res, err := WriteFile{Path: "x.txt", Data: []byte("same")}.Run(context.Background(),
		OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, ok := repo.written["x.txt"]; ok {
		t.Fatalf("identical content should not be rewritten")
	}
	if res.Changed {
		t.Fatalf("no-op should report Changed=false")
	}
}

func TestWriteFileForksOnExistingDiffering(t *testing.T) {
	// Cancel -> no write, ErrWriteCancelled.
	repo := newWriteFake()
	repo.existing["x.txt"] = []byte("old")
	_, err := WriteFile{Path: "x.txt", Data: []byte("new")}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"overwrite": writeCancel}})
	if !errors.Is(err, ErrWriteCancelled) {
		t.Fatalf("cancel err = %v, want ErrWriteCancelled", err)
	}
	if _, ok := repo.written["x.txt"]; ok {
		t.Fatalf("cancel must not write")
	}

	// Overwrite -> writes new bytes.
	repo2 := newWriteFake()
	repo2.existing["x.txt"] = []byte("old")
	if _, err := (WriteFile{Path: "x.txt", Data: []byte("new")}).Run(context.Background(),
		OpDeps{Repo: repo2, Decider: MapDecider{"overwrite": writeOverwrite}}); err != nil {
		t.Fatalf("overwrite run: %v", err)
	}
	if string(repo2.written["x.txt"]) != "new" {
		t.Fatalf("overwrite should write new bytes, got %q", repo2.written["x.txt"])
	}
}
