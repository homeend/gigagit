package git

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/changeset"
)

func TestDiffNameStatusReportsStatusesAndIgnoresRenames(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()

	writeFile(t, dir, "keep.txt", "one\n")
	writeFile(t, dir, "moved.txt", "content that is long enough to be detected as a rename\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
	base := revParse(t, dir, "HEAD")

	writeFile(t, dir, "keep.txt", "two\n")
	gitRun(t, dir, "rm", "-q", "moved.txt")
	writeFile(t, dir, "renamed.txt", "content that is long enough to be detected as a rename\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "change")
	head := revParse(t, dir, "HEAD")

	got, err := r.DiffNameStatus(ctx, base, head)
	if err != nil {
		t.Fatalf("DiffNameStatus: %v", err)
	}

	// --no-renames is mandatory: with detection on, the D+A pair below would
	// collapse into a single R entry and manufacture phantom set differences
	// against the other side of the comparison.
	want := map[changeset.Entry]bool{
		{Status: 'M', Path: "keep.txt"}:    true,
		{Status: 'D', Path: "moved.txt"}:   true,
		{Status: 'A', Path: "renamed.txt"}: true,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries %v, want %d", len(got), got, len(want))
	}
	for _, e := range got {
		if !want[e] {
			t.Errorf("unexpected entry %v (a rename was detected, or a status is wrong)", e)
		}
	}
}

func TestParseNameStatusEntriesSkipsRenameTripleWithoutShifting(t *testing.T) {
	t.Parallel()
	// R100/old/new is the rename shape --no-renames should make unreachable in
	// practice; the parser still must consume all three fields as one unit
	// (not emit an R entry, and not let the M/k.txt pair that follows shift
	// out of alignment).
	got := parseNameStatusEntries("R100\x00old.txt\x00new.txt\x00M\x00k.txt\x00")
	want := []changeset.Entry{{Status: 'M', Path: "k.txt"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseNameStatusEntriesStopsOnTruncatedTail(t *testing.T) {
	t.Parallel()
	// A trailing status with no path (or an R/C with a missing new-path field)
	// must be dropped, not panic on an out-of-range index.
	got := parseNameStatusEntries("M\x00a.txt\x00D\x00")
	want := []changeset.Entry{{Status: 'M', Path: "a.txt"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDiffNameStatusEmptyForIdenticalTrees(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	head := revParse(t, dir, "HEAD")

	got, err := r.DiffNameStatus(context.Background(), head, head)
	if err != nil {
		t.Fatalf("DiffNameStatus: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want no entries", got)
	}
}
