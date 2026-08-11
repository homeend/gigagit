package domain

import (
	"context"
	"strings"
	"testing"
)

// ResolvePrefixValue previews a prefix against the live repo: tokens resolve,
// user inputs substitute, and — critically — seq counters are PEEKED, never
// consumed. Canceling a prefill must not burn a number.
func TestResolvePrefixValuePeeksSeqs(t *testing.T) {
	_, svc := newRealRepo(t)
	ctx := context.Background()

	out, names, err := svc.ResolvePrefixValue(ctx, "feat/x-<seq:ctr:3>", nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if out != "feat/x-001" {
		t.Errorf("resolved = %q, want feat/x-001", out)
	}
	if len(names) != 1 || names[0] != "ctr" {
		t.Errorf("seq names = %v", names)
	}
	// resolving again yields the SAME number — nothing was consumed
	out2, _, err := svc.ResolvePrefixValue(ctx, "feat/x-<seq:ctr:3>", nil)
	if err != nil || out2 != out {
		t.Errorf("second resolve = %q, err %v — a resolve must not bump", out2, err)
	}
}

func TestResolvePrefixValueTokens(t *testing.T) {
	_, svc := newRealRepo(t)
	ctx := context.Background()

	// <user:…> substitutes the provided input; <parent-branch> is the current branch.
	out, names, err := svc.ResolvePrefixValue(ctx, "<parent-branch>/<user:ticket>-", map[string]string{"ticket": "AB-12"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if out != "main/AB-12-" {
		t.Errorf("resolved = %q, want main/AB-12-", out)
	}
	if len(names) != 0 {
		t.Errorf("seq names = %v, want none", names)
	}

	// a malformed value reports, not panics
	if _, _, err := svc.ResolvePrefixValue(ctx, "feat/<nope:x>", nil); err == nil {
		t.Error("want error for an unknown token")
	}
}

// BumpPrefixSeqs consumes the counters, so the next resolve moves on.
func TestBumpPrefixSeqsAdvances(t *testing.T) {
	_, svc := newRealRepo(t)
	ctx := context.Background()

	before, _, err := svc.ResolvePrefixValue(ctx, "x-<seq:n:2>", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.BumpPrefixSeqs(ctx, []string{"n"}); err != nil {
		t.Fatalf("bump: %v", err)
	}
	after, _, err := svc.ResolvePrefixValue(ctx, "x-<seq:n:2>", nil)
	if err != nil {
		t.Fatal(err)
	}
	if before != "x-01" || after != "x-02" {
		t.Errorf("before/after = %q / %q, want x-01 / x-02", before, after)
	}
}

// PrefixUserLabels is the frontends' route to a value's <user:…> labels
// (internal/template is a layering detail they must not reach for).
func TestPrefixUserLabels(t *testing.T) {
	got := PrefixUserLabels("a/<user:ticket>-<user:who>/<seq:n:2>")
	if strings.Join(got, ",") != "ticket,who" {
		t.Errorf("labels = %v", got)
	}
	if got := PrefixUserLabels("plain/"); len(got) != 0 {
		t.Errorf("labels = %v, want none", got)
	}
}
