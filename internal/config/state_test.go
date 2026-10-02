package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPeekSeqUnsetIsOne(t *testing.T) {
	gitDir := t.TempDir()
	if n := PeekSeq(gitDir, "issue"); n != 1 {
		t.Fatalf("unset PeekSeq = %d, want 1 (1-based)", n)
	}
}

func TestBumpSeqSequence(t *testing.T) {
	gitDir := t.TempDir()
	// First consume -> 1, matching the prior Peek.
	if p := PeekSeq(gitDir, "issue"); p != 1 {
		t.Fatalf("peek before first bump = %d, want 1", p)
	}
	n, err := BumpSeq(gitDir, "issue")
	if err != nil {
		t.Fatalf("bump: %v", err)
	}
	if n != 1 {
		t.Fatalf("first BumpSeq = %d, want 1", n)
	}
	// Now next peek is 2, and second bump consumes 2.
	if p := PeekSeq(gitDir, "issue"); p != 2 {
		t.Fatalf("peek after first bump = %d, want 2", p)
	}
	n2, _ := BumpSeq(gitDir, "issue")
	if n2 != 2 {
		t.Fatalf("second BumpSeq = %d, want 2", n2)
	}
}

func TestBumpSeqPersistsAndIsIsolatedPerName(t *testing.T) {
	gitDir := t.TempDir()
	if _, err := BumpSeq(gitDir, "issue"); err != nil { // issue -> 1
		t.Fatal(err)
	}
	if _, err := BumpSeq(gitDir, "issue"); err != nil { // issue -> 2
		t.Fatal(err)
	}
	if _, err := BumpSeq(gitDir, "deploy"); err != nil { // deploy -> 1 (separate)
		t.Fatal(err)
	}
	// A fresh read (new process simulation) sees the persisted values.
	if p := PeekSeq(gitDir, "issue"); p != 3 {
		t.Errorf("persisted issue next = %d, want 3", p)
	}
	if p := PeekSeq(gitDir, "deploy"); p != 2 {
		t.Errorf("persisted deploy next = %d, want 2", p)
	}
}

// Regression: writing must overwrite an existing state.toml (os.Rename replace
// semantics), since the dev/CI drive is Windows-mounted and cross-platform is
// a hard requirement.
func TestBumpSeqOverwritesExistingFile(t *testing.T) {
	gitDir := t.TempDir()
	statePath := filepath.Join(gitDir, "gg", "state.toml")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("[seq]\nissue = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := BumpSeq(gitDir, "issue")
	if err != nil {
		t.Fatalf("bump over existing file: %v", err)
	}
	if n != 6 {
		t.Fatalf("BumpSeq over existing = %d, want 6", n)
	}
	if p := PeekSeq(gitDir, "issue"); p != 7 {
		t.Fatalf("peek after = %d, want 7", p)
	}
}

// The state file lives under .git/gg/, never the committed config.
func TestBumpSeqWritesUnderGitDirOnly(t *testing.T) {
	gitDir := t.TempDir()
	if _, err := BumpSeq(gitDir, "issue"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(gitDir, "gg", "state.toml")); err != nil {
		t.Fatalf("state.toml not written under gitDir/gg: %v", err)
	}
}

// Regression: an empty gitDir must error rather than write a stray gg/state.toml
// relative to the process working directory.
func TestBumpSeqEmptyDirErrors(t *testing.T) {
	if _, err := BumpSeq("", "issue"); err == nil {
		t.Fatal("BumpSeq with empty gitDir should error, not write")
	}
	// And no stray gg/ dir was created in the CWD.
	if _, statErr := os.Stat("gg"); statErr == nil {
		_ = os.RemoveAll("gg")
		t.Fatal("BumpSeq wrote a stray gg/ directory in the CWD")
	}
}

// Several processes (agents) bump one counter at once: each must get its
// own number.
func TestBumpSeqConcurrentHandsOutDistinctNumbers(t *testing.T) {
	gitDir := t.TempDir()
	const n = 16
	got := make(chan int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := BumpSeq(gitDir, "issue")
			if err != nil {
				t.Error(err)
			}
			got <- v
		}()
	}
	wg.Wait()
	close(got)
	seen := map[int]bool{}
	for v := range got {
		if seen[v] {
			t.Fatalf("number %d handed out twice", v)
		}
		seen[v] = true
	}
	if PeekSeq(gitDir, "issue") != n+1 {
		t.Fatalf("next = %d, want %d", PeekSeq(gitDir, "issue"), n+1)
	}
}

// BumpSeqs consumes several counters in ONE write: either all advance or none.
func TestBumpSeqsAdvancesAllInOneWrite(t *testing.T) {
	dir := t.TempDir()
	if _, err := BumpSeq(dir, "a"); err != nil {
		t.Fatal(err)
	}
	got, err := BumpSeqs(dir, []string{"a", "b", "a"})
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != 2 || got["b"] != 1 || len(got) != 2 {
		t.Fatalf("BumpSeqs = %v, want a:2 b:1", got)
	}
	if PeekSeq(dir, "a") != 3 || PeekSeq(dir, "b") != 2 {
		t.Fatalf("after: a=%d b=%d", PeekSeq(dir, "a"), PeekSeq(dir, "b"))
	}
}

// A state file that cannot be read fails the whole take: no counter moves.
func TestBumpSeqsDamagedStateConsumesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "gg"), 0o755); err != nil {
		t.Fatal(err)
	}
	damaged := []byte("[seq\na = 1\n")
	if err := os.WriteFile(statePath(dir), damaged, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := BumpSeqs(dir, []string{"a", "b"}); err == nil {
		t.Fatal("BumpSeqs on a damaged state file: no error")
	}
	if got, _ := os.ReadFile(statePath(dir)); string(got) != string(damaged) {
		t.Fatalf("state rewritten: %s", got)
	}
	if _, err := BumpSeqs("", []string{"a"}); err == nil {
		t.Fatal("BumpSeqs with no git dir: no error")
	}
}
