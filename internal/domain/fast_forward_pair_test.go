package domain

import (
	"context"
	"testing"
)

// One branch strictly behind the other: the pair resolves in both argument
// orders, always naming the behind branch as Behind.
func TestFastForwardPairBehindAhead(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	gitIn(t, dir, "branch", "old") // stays at the initial commit
	writeIn(t, dir, "a.txt", "a\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "main ahead")

	for _, args := range [][2]string{{"old", "main"}, {"main", "old"}} {
		p, err := svc.FastForwardPair(ctx, args[0], args[1])
		if err != nil {
			t.Fatalf("FastForwardPair(%v): %v", args, err)
		}
		if !p.OK || p.Behind != "old" || p.Ahead != "main" {
			t.Fatalf("FastForwardPair(%v) = %+v, want behind=old ahead=main", args, p)
		}
	}
}

// Equal tips and diverged branches both report no fast-forward.
func TestFastForwardPairNone(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	gitIn(t, dir, "branch", "twin") // same tip
	if p, err := svc.FastForwardPair(ctx, "twin", "main"); err != nil || p.OK {
		t.Fatalf("equal tips: p=%+v err=%v, want OK=false", p, err)
	}

	gitIn(t, dir, "checkout", "-b", "feat")
	writeIn(t, dir, "f.txt", "f\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "feat side")
	gitIn(t, dir, "checkout", "main")
	writeIn(t, dir, "m.txt", "m\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "main side")

	if p, err := svc.FastForwardPair(ctx, "feat", "main"); err != nil || p.OK {
		t.Fatalf("diverged: p=%+v err=%v, want OK=false", p, err)
	}
}
