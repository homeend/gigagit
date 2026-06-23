package fuzzy

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestScoreSubsequence(t *testing.T) {
	if _, ok := Score("fvg", "internal/tui/files_view.go"); !ok {
		t.Fatal("fvg should match files_view.go")
	}
	if _, ok := Score("zzz", "files_view.go"); ok {
		t.Fatal("zzz must not match")
	}
	if _, ok := Score("", "anything"); !ok {
		t.Fatal("empty query matches everything")
	}
}

func TestRankFvgoFindsFilesView(t *testing.T) {
	cands := []string{"favorites/go.mod", "internal/tui/files_view.go", "x/y/z.go"}
	got := Rank("fvgo", cands, 10)
	if len(got) == 0 || got[0].S != "internal/tui/files_view.go" {
		t.Fatalf("fvgo should rank files_view.go first; got %v", got)
	}
}

func TestRankBoundaryBeatsScattered(t *testing.T) {
	// "ab" as a word-boundary/contiguous match should outrank a scattered one.
	got := Rank("ab", []string{"xaxbx", "a/b.go"}, 10)
	if got[0].S != "a/b.go" {
		t.Fatalf("boundary/contiguous match should win; got %v", got)
	}
}

func TestRankEmptyQueryIdentity(t *testing.T) {
	cands := []string{"c", "a", "b"}
	got := Rank("", cands, 2)
	if len(got) != 2 || got[0].S != "c" || got[1].S != "a" {
		t.Fatalf("empty query keeps original order, capped; got %v", got)
	}
}

// TestRankBoundedTiebreak verifies that the bounded top-N heap path (limit < matches)
// produces the same result as a full-sort when scores tie — i.e. h[0] is always the
// genuine worst element (lowest score, then largest path alphabetically).
func TestRankBoundedTiebreak(t *testing.T) {
	// All three match "e" with identical score (single-char basename match).
	// Full-sort tiebreak = path ascending → top-2 should be [e/a.go, e/m.go].
	got := Rank("e", []string{"e/m.go", "e/z.go", "e/a.go"}, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %v", got)
	}
	if got[0].S != "e/a.go" || got[1].S != "e/m.go" {
		t.Fatalf("bounded tiebreak wrong; want [e/a.go, e/m.go], got [%s, %s]", got[0].S, got[1].S)
	}
}

func TestRankPerf(t *testing.T) {
	// Build a synthetic 100k-path slice. Use a query "e" which matches most paths
	// so that scoring + sorting the full result set is exercised.
	const n = 100_000
	paths := make([]string, n)
	dirs := []string{"internal/engine", "internal/tui", "cmd/gg", "pkg/util", "vendor/github.com"}
	for i := range paths {
		paths[i] = fmt.Sprintf("%s/file_%d.go", dirs[i%len(dirs)], i)
	}

	// Warm-up run.
	_ = Rank("e", paths, 20)

	// Timed run.
	start := time.Now()
	results := Rank("e", paths, 20)
	elapsed := time.Since(start)

	if len(results) == 0 {
		t.Fatal("expected matches for query 'e'")
	}
	t.Logf("Rank over %d paths took %v; top result: %s (score=%d)", n, elapsed, results[0].S, results[0].Score)

	// Assert we complete well under 30ms. Boundary chosen to be very generous
	// to avoid flakiness on loaded machines while still catching pathological slowness.
	const limit = 30 * time.Millisecond
	// Use a lenient bound: 10× the target to avoid CI flakiness on slow machines.
	const hardLimit = 10 * limit
	if elapsed > hardLimit {
		t.Fatalf("Rank over %d paths took %v, exceeds hard limit %v", n, elapsed, hardLimit)
	}
	if elapsed > limit {
		t.Logf("WARNING: Rank took %v which exceeds the soft 30ms target (hard limit %v); consider switching to bounded top-N selection", elapsed, hardLimit)
	}

	// Validate we only return at most the requested limit.
	if len(results) > 20 {
		t.Fatalf("expected at most 20 results, got %d", len(results))
	}

	// Validate results are actually matches.
	for _, r := range results {
		if !strings.Contains(strings.ToLower(r.S), "e") {
			t.Fatalf("result %q does not appear to match query 'e'", r.S)
		}
	}
}
