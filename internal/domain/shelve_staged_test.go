package domain

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/shelf"
)

// recycleFixture is a repo whose second worktree wt (on branch feat) holds
// committed old.txt, gone.txt and keep.txt; edit applies the worktree's
// changes, then the whole tree is staged as the recycle op does (add -A).
func recycleFixture(t *testing.T, edit func(wt string)) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	write := func(root, name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gittest.Run(t, dir, "init", "-b", "main")
	write(dir, "keep.txt", "keep\n")
	write(dir, "gone.txt", "gone\n")
	write(dir, "old.txt", "renamed body that is long enough to be detected\n")
	gittest.Run(t, dir, "add", "-A")
	gittest.Run(t, dir, "commit", "-m", "root")
	gittest.Run(t, dir, "branch", "feat")
	wt := filepath.Join(t.TempDir(), "wt")
	gittest.Run(t, dir, "worktree", "add", wt, "feat")
	edit(wt)
	gittest.Run(t, wt, "add", "-A")
	svc := svcIn(t, dir)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	svc.UseNotesDir(t.TempDir())
	return svc, wt
}

// tarMembers reads a stored set back: name → content.
func tarMembers(t *testing.T, svc *Service, id string) map[string]string {
	t.Helper()
	blob, err := svc.ShelfBlob(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(blob))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		out[h.Name] = string(b)
	}
	return out
}

func keys(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func TestShelveStagedStoresIndexAsOneSet(t *testing.T) {
	t.Parallel()
	svc, wt := recycleFixture(t, func(wt string) {
		os.WriteFile(filepath.Join(wt, "keep.txt"), []byte("kept, edited\n"), 0o644)
		os.WriteFile(filepath.Join(wt, "fresh.txt"), []byte("fresh\n"), 0o644)
		os.Remove(filepath.Join(wt, "gone.txt"))
		os.Rename(filepath.Join(wt, "old.txt"), filepath.Join(wt, "moved.txt"))
	})
	ctx := context.Background()
	e, err := svc.shelveStagedIn(ctx, wt, "feat")
	if err != nil {
		t.Fatalf("shelveStagedIn: %v", err)
	}
	if e.Label != "WIP on feat" || e.Kind != model.ShelfKindFiles || e.Origin.Worktree != wt || e.Origin.State != model.StateStaged {
		t.Fatalf("entry = %+v", e)
	}
	m := tarMembers(t, svc, e.ID)
	if got := strings.Join(keys(m), ","); got != "fresh.txt,keep.txt,moved.txt" {
		t.Fatalf("members = %s", got)
	}
	if m["keep.txt"] != "kept, edited\n" {
		t.Fatalf("keep.txt = %q, want the index bytes", m["keep.txt"])
	}
	ns, _ := svc.ShelfNotes(ctx, e.ID)
	if len(ns) != 1 {
		t.Fatalf("want one note, got %d", len(ns))
	}
	n := ns[0].Note
	want := "Deleted (not in this set):\n  gone.txt\nRenamed (stored under the new path):\n  old.txt → moved.txt"
	if n.Rationale != want || n.Author != "gg" || n.Source != model.NoteSourceAgent || n.Summary != "Recycled from "+wt+" (feat)" {
		t.Fatalf("note = %+v\nwant rationale %q", n, want)
	}
}

func TestShelveStagedNoNoteWhenOnlyModifications(t *testing.T) {
	t.Parallel()
	svc, wt := recycleFixture(t, func(wt string) {
		os.WriteFile(filepath.Join(wt, "keep.txt"), []byte("edited\n"), 0o644)
	})
	e, err := svc.shelveStagedIn(context.Background(), wt, "feat")
	if err != nil {
		t.Fatal(err)
	}
	if ns, _ := svc.ShelfNotes(context.Background(), e.ID); len(ns) != 0 {
		t.Fatalf("a set that carries everything needs no note, got %+v", ns)
	}
}

func TestShelveStagedAllDeletionsAddsPlaceholder(t *testing.T) {
	t.Parallel()
	svc, wt := recycleFixture(t, func(wt string) {
		os.Remove(filepath.Join(wt, "gone.txt"))
	})
	ctx := context.Background()
	e, err := svc.shelveStagedIn(ctx, wt, "feat")
	if err != nil {
		t.Fatal(err)
	}
	m := tarMembers(t, svc, e.ID)
	if got := strings.Join(keys(m), ","); got != "delete.me" || m["delete.me"] != "" {
		t.Fatalf("members = %s, want only an empty delete.me", got)
	}
	ns, _ := svc.ShelfNotes(ctx, e.ID)
	if len(ns) != 1 || !strings.HasPrefix(ns[0].Note.Rationale, "delete.me is an empty placeholder: every change in this worktree was a deletion, and a shelf set cannot be empty.\nDeleted (not in this set):\n  gone.txt") {
		t.Fatalf("notes = %+v", ns)
	}
}

func TestShelveStagedSpaceAndUnicodePaths(t *testing.T) {
	t.Parallel()
	svc, wt := recycleFixture(t, func(wt string) {
		os.WriteFile(filepath.Join(wt, "a b.txt"), []byte("space\n"), 0o644)
		os.WriteFile(filepath.Join(wt, "ünï.txt"), []byte("uni\n"), 0o644)
	})
	e, err := svc.shelveStagedIn(context.Background(), wt, "feat")
	if err != nil {
		t.Fatal(err)
	}
	m := tarMembers(t, svc, e.ID)
	if m["a b.txt"] != "space\n" || m["ünï.txt"] != "uni\n" {
		t.Fatalf("members = %v", keys(m))
	}
}

// shelveOp calls the seam from inside an op, as RecycleWorktree does.
type shelveOp struct{ dir string }

func (o shelveOp) Run(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
	if deps.ShelveStaged == nil {
		return engine.Result{}, engine.ErrNoShelve
	}
	_, err := deps.ShelveStaged(ctx, o.dir, "feat")
	return engine.Result{}, err
}

func TestShelveStagedThroughExecuteDoesNotDeadlock(t *testing.T) {
	t.Parallel()
	svc, wt := recycleFixture(t, func(wt string) {
		os.WriteFile(filepath.Join(wt, "keep.txt"), []byte("edited\n"), 0o644)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := svc.Execute(ctx, shelveOp{dir: wt}, nil, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if es, _ := svc.ShelfList(context.Background(), "", 0, 0); len(es) != 1 {
		t.Fatalf("want the set stored, got %d entries", len(es))
	}
}
