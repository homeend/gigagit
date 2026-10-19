package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
)

// A standalone server attaches the served worktree at Start and lists what
// its directory already holds; Close detaches without unlinking anything.
func TestStandaloneAttachesAtStartAndClosesItsStore(t *testing.T) {
	isolateGlobal(t)
	isolateState(t)
	s := newSteerServer(t)
	dir := filepath.Join(t.TempDir(), "docs")
	s.docsDirOf = func(_, _ string) string { return dir }
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	h := newHostOver(s, false)
	if _, err := h.Start(context.Background(), "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	root := s.docsRoot(context.Background())
	if got := s.docs.Attached(root); got != dir {
		t.Fatalf("Attached(%s) = %q, want %q", root, got, dir)
	}
	if _, ok := s.ofs.resolve(s.service().Root(), "20260101T220000-ab12", ""); !ok {
		t.Fatal("the directory's document is not listed after Start")
	}
	h.Close()
	if s.docs.Attached(root) != "" {
		t.Fatal("Close left the root attached")
	}
	if _, err := os.Stat(filepath.Join(dir, "20260101T220000-ab12.md")); err != nil {
		t.Fatal("Close unlinked a document")
	}
}

// Review Focus 3: a re-root detaches the old worktree and attaches the new
// one; the old directory's files are untouched.
func TestRerootDetachesTheOldRootAndAttachesTheNew(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	dirs := map[string]string{}
	s.docsDirOf = func(_, top string) string {
		k := domain.CheckoutKey(top)
		if _, ok := dirs[k]; !ok {
			dirs[k] = filepath.Join(t.TempDir(), "docs")
		}
		return dirs[k]
	}
	s.attachDocs(context.Background())
	rootA := s.docsRoot(context.Background())
	dirA := s.docs.Attached(rootA)
	if dirA == "" {
		t.Fatal("not attached")
	}
	t.Cleanup(s.docs.Close)
	writeDoc(t, dirA, "20260101T220000-ab12.md", tourDoc)
	s.docs.Reload(rootA)
	s.followDocs()
	other := domain.Open(newRepoDir(t, 2))
	if err := s.adoptService(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	rootB := s.docsRoot(context.Background())
	if rootB == rootA {
		t.Fatal("the test needs two worktrees")
	}
	if s.docs.Attached(rootA) != "" {
		t.Fatal("the old root is still attached")
	}
	if s.docs.Attached(rootB) == "" {
		t.Fatal("the new root is not attached")
	}
	if _, ok := s.ofs.resolve(other.Root(), "20260101T220000-ab12", ""); ok {
		t.Fatal("the old worktree's document is listed under the new one")
	}
	if _, err := os.Stat(filepath.Join(dirA, "20260101T220000-ab12.md")); err != nil {
		t.Fatal("the re-root unlinked a document")
	}
}

// Review Focus 2: a hosted page shares the TUI's store and never attaches,
// detaches or closes it — Start, Reroot and Close leave a root the TUI
// attached as it was.
func TestHostedPageNeverTouchesTheSharedStore(t *testing.T) {
	isolateGlobal(t) // agentdocs.Shared() is process-global: not parallel
	isolateState(t)
	svc := domain.Open(newRepoDir(t, 1))
	top, err := svc.TopLevel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	root := domain.CheckoutKey(top)
	dir := filepath.Join(t.TempDir(), "docs")
	if err := agentdocs.Shared().Attach(root, top, dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { agentdocs.Shared().Detach(root) })
	h := NewHost(svc, domain.Open, true)
	h.srv.docsDirOf = func(_, _ string) string { t.Error("a hosted page resolved a docs dir"); return "" }
	if _, err := h.Start(context.Background(), "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	if err := h.Reroot(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	h.Close()
	if agentdocs.Shared().Attached(root) != dir {
		t.Fatal("the hosted page detached the TUI's root")
	}
}
