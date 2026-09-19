package engine

import (
	"context"
	"errors"
	"testing"
)

// countingAction stands in for any future action: ApplyMigration must run
// WHATEVER it was handed, not a body of its own.
type countingAction struct {
	n      int
	err    error
	called bool
}

func (a *countingAction) Describe() string { return "counted" }
func (a *countingAction) Apply(ctx context.Context, deps OpDeps) (int, error) {
	a.called = true
	return a.n, a.err
}

// ApplyMigration is a dumb executor: it runs the action it was handed and
// then stamps the marker. Nothing about WHICH store is being migrated may
// live in the op.
func TestApplyMigrationRunsWhateverActionItWasHanded(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	ctx := context.Background()
	deps := OpDeps{Repo: repo}

	act := &countingAction{n: 3}
	op := ApplyMigration{Feature: "f", Store: "previews", To: 2, Action: act}
	res, err := op.Run(ctx, deps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !act.called {
		t.Fatal("ApplyMigration did not run its action")
	}
	formats, err := repo.StoreFormats(ctx)
	if err != nil {
		t.Fatalf("StoreFormats: %v", err)
	}
	if formats["previews"] != 2 {
		t.Fatalf("StoreFormats = %v, want previews=2", formats)
	}
	if !res.Changed {
		t.Fatal("Result.Changed is false")
	}
}

// A failing action aborts BEFORE the marker is stamped: a half-migrated store
// must not be labelled migrated.
func TestApplyMigrationDoesNotStampWhenTheActionFails(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	ctx := context.Background()
	deps := OpDeps{Repo: repo}

	boom := errors.New("boom")
	op := ApplyMigration{Feature: "f", Store: "previews", To: 2, Action: &countingAction{err: boom}}
	if _, err := op.Run(ctx, deps); !errors.Is(err, boom) {
		t.Fatalf("Run err = %v, want boom", err)
	}
	formats, err := repo.StoreFormats(ctx)
	if err != nil {
		t.Fatalf("StoreFormats: %v", err)
	}
	if _, stamped := formats["previews"]; stamped {
		t.Fatalf("the marker was stamped after the action failed: %v", formats)
	}
}

// A nil action is a caller bug, refused like the other zero-value guards —
// and refused by RETURNING an error, never by panicking: a panic is not a
// pass, and a frontend calling this must get an error it can report.
func TestApplyMigrationRefusesANilAction(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	op := ApplyMigration{Feature: "f", Store: "previews", To: 2}
	if _, err := op.Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("Run accepted a nil Action")
	}
}

// DiscardRefs is today's behaviour, extracted verbatim: it deletes exactly
// the refs it was handed, in order, and reports the count.
func TestDiscardRefsDeletesExactlyWhatItWasHanded(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	ctx := context.Background()
	deps := OpDeps{Repo: repo}

	head, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	// A ref the action was NOT handed, to prove it deletes exactly its own
	// list rather than everything under the prefix.
	keep := "refs/gg/versions/main/1700000002-keep"
	refs := []string{"refs/gg/versions/main/1700000000-a", "refs/gg/versions/main/1700000001-b"}
	for _, ref := range append(append([]string{}, refs...), keep) {
		if err := repo.UpdateRef(ctx, ref, head); err != nil {
			t.Fatal(err)
		}
	}

	n, err := DiscardRefs{Refs: refs}.Apply(ctx, deps)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}
	left, err := repo.ForEachRef(ctx, "refs/gg/versions/")
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	if len(left) != 1 || left[0].Ref != keep {
		t.Fatalf("surviving refs = %v, want exactly %q", left, keep)
	}
}
