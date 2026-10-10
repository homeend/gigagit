package domain

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

// A diff computed through worktree A's service is a HIT through B's: the
// two services vend their "diff" cache from one factory. The second request
// deliberately differs in content under the same key, so a miss would show.
func TestSharedServicesHitOneDiffCache(t *testing.T) {
	t.Parallel()
	dir, a := newRealRepo(t)
	_, other := newRealRepoAt(t, dir)
	b := NewSharing(other.repo, a)
	first, err := a.Differ().Diff(context.Background(), Request{Key: "shared:k", Path: "a.go", Old: src([]byte("x\n")), New: src([]byte("y\n"))})
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.Differ().Diff(context.Background(), Request{Key: "shared:k", Path: "a.go", Old: src([]byte("x\n")), New: src([]byte("z\n"))})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second, first) { // Diff is a value with slices; a hit is the very same answer, a miss shows the "z"
		t.Fatal("B computed its own diff: the caches are not shared")
	}
}

// A commit's file list read through A is cached for B.
func TestSharedServicesShareTheCommitFilesCache(t *testing.T) {
	t.Parallel()
	dir, a := newRealRepo(t)
	_, other := newRealRepoAt(t, dir)
	b := NewSharing(other.repo, a)
	head := headHash(t, dir)
	if _, err := a.CommitFiles(context.Background(), head); err != nil {
		t.Fatal(err)
	}
	if !b.CommitFilesCached(head) {
		t.Fatal("B does not see A's commit-files entry")
	}
}

// Plain constructors keep a private factory.
func TestNewServiceDoesNotShareCaches(t *testing.T) {
	t.Parallel()
	dir, a := newRealRepo(t)
	_, b := newRealRepoAt(t, dir)
	first, _ := a.Differ().Diff(context.Background(), Request{Key: "private:k", Path: "a.go", Old: src([]byte("x\n")), New: src([]byte("y\n"))})
	second, _ := b.Differ().Diff(context.Background(), Request{Key: "private:k", Path: "a.go", Old: src([]byte("x\n")), New: src([]byte("z\n"))})
	if reflect.DeepEqual(second, first) {
		t.Fatal("two plain services share a cache")
	}
}

// Two services filling one factory concurrently: -race must stay quiet.
func TestSharedCachesFromTwoServicesRace(t *testing.T) {
	t.Parallel()
	dir, a := newRealRepo(t)
	_, other := newRealRepoAt(t, dir)
	b := NewSharing(other.repo, a)
	var wg sync.WaitGroup
	for _, s := range []*Service{a, b} {
		wg.Add(1)
		go func(s *Service) {
			defer wg.Done()
			for i := range 50 {
				_, _ = s.Differ().Diff(context.Background(), Request{Key: "race:" + string(rune('a'+i%7)), Path: "a.go", Old: src([]byte("x\n")), New: src([]byte("y\n"))})
			}
		}(s)
	}
	wg.Wait()
}
