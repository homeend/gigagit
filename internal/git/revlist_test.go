package git

import (
	"context"
	"testing"
)

func TestRevListRangeListsCommitsNewestFirst(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()
	writeFile(t, dir, "a.txt", "one\n")
	commitAll(t, dir, "c1")
	base, err := r.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "a.txt", "two\n")
	commitAll(t, dir, "c2")
	writeFile(t, dir, "a.txt", "three\n")
	commitAll(t, dir, "c3")
	tip, err := r.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	got, err := r.RevListRange(ctx, base, tip)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 commits between base and tip, got %d: %v", len(got), got)
	}
	if got[0] != tip {
		t.Fatalf("newest first: want %s, got %s", tip, got[0])
	}
	for _, c := range got {
		if len(c) != 40 {
			t.Fatalf("want full shas, got %q", c)
		}
	}
}

func TestRevListRangeEmptyWhenTipIsBase(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()
	writeFile(t, dir, "a.txt", "one\n")
	commitAll(t, dir, "c1")
	h, err := r.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.RevListRange(ctx, h, h)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want no commits, got %v", got)
	}
}
