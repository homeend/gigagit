package domain

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/preflight"
)

func TestPreflightSatisfiedOnAFreshRepo(t *testing.T) {
	t.Parallel()
	svc := svcAt(cleanDir(t))

	vs, err := svc.Preflight(context.Background())
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if len(vs) == 0 {
		t.Fatal("Preflight returned no verdicts")
	}
	for _, v := range vs {
		if v.State != preflight.Satisfied {
			t.Errorf("feature %q = %v, want Satisfied (%s)", v.Feature.ID, v.State, v.Reason.Format)
		}
	}
}

func TestPreflightTreatsUnmarkedVersionRefsAsLegacyFormatOne(t *testing.T) {
	t.Parallel()
	svc := svcAt(cleanDir(t))
	ctx := context.Background()

	// A repo from before markers existed: a version ref, no marker.
	head, err := svc.Repo().RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	if err := svc.Repo().UpdateRef(ctx, "refs/gg/versions/main/1700000000-rebase", head); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	p, _, err := svc.preflightProbes(ctx)
	if err != nil {
		t.Fatalf("preflightProbes: %v", err)
	}
	got := p.Stores[StoreVersions]
	if got.Format != 0 || !got.HasData {
		t.Errorf("versions probe = %+v, want {Format:0 HasData:true}", got)
	}

	// With this build at format 1, legacy data resolves as Satisfied. The
	// follow-up spec bumps the range to 2, which turns this into Repairable.
	if fit := (preflight.DataFormat{Store: StoreVersions, Min: 2, Max: 2}).Fit(p); fit != preflight.FitTooOld {
		t.Errorf("fit against a format-2 build = %v, want FitTooOld", fit)
	}
}

// countingRunner counts git invocations by name so a test can prove which
// probes a Preflight call actually paid for.
type countingRunner struct {
	inner gitexec.Runner
	mu    sync.Mutex
	calls map[string]int
}

func newCountingRunner(inner gitexec.Runner) *countingRunner {
	return &countingRunner{inner: inner, calls: map[string]int{}}
}

func (c *countingRunner) note(name string) {
	c.mu.Lock()
	c.calls[name]++
	c.mu.Unlock()
}

func (c *countingRunner) count(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[name]
}

func (c *countingRunner) reset() {
	c.mu.Lock()
	c.calls = map[string]int{}
	c.mu.Unlock()
}

func (c *countingRunner) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	c.note(name)
	return c.inner.Run(ctx, name, argv)
}

func (c *countingRunner) RunEnv(ctx context.Context, name string, argv, env []string) (gitexec.Result, error) {
	c.note(name)
	return c.inner.RunEnv(ctx, name, argv, env)
}

func (c *countingRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (gitexec.Result, error) {
	c.note(name)
	return c.inner.Stream(ctx, name, argv, onLine)
}

// TestPreflightIsCached pins the steady state: a second call re-reads ONLY the
// store markers (the cache validation) and re-runs neither the git-version nor
// the version-ref probe. Counting the invocations keeps it from passing
// vacuously now that a cache hit is no longer a no-op.
func TestPreflightIsCached(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	cr := newCountingRunner(gitexec.NewExecRunner("git", dir, observ.NewRing(50)))
	svc := New(&git.Repo{Runner: cr})
	ctx := context.Background()

	a, err := svc.Preflight(ctx)
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if n := cr.count("git version"); n != 1 {
		t.Errorf("first Preflight ran git version %d times, want 1", n)
	}
	if n := cr.count("git for-each-ref (gg)"); n != 2 {
		t.Errorf("first Preflight ran for-each-ref %d times, want 2 (markers + version refs)", n)
	}

	cr.reset()
	b, err := svc.Preflight(ctx)
	if err != nil {
		t.Fatalf("Preflight (second call): %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("cached verdicts differ: %d then %d", len(a), len(b))
	}
	if n := cr.count("git version"); n != 0 {
		t.Errorf("cached Preflight re-ran git version %d times, want 0", n)
	}
	if n := cr.count("git for-each-ref (gg)"); n != 1 {
		t.Errorf("cached Preflight ran for-each-ref %d times, want exactly 1 (the marker re-read)", n)
	}
	if !svc.preflightDone {
		t.Error("preflightDone = false after Preflight; the result was not cached")
	}
}

// TestPreflightReResolvesWhenAnotherProcessChangesAMarker is the out-of-process
// case: several gg processes share one repo as peers, so a marker written by a
// peer (here, a raw ref write) must invalidate this Service's cached verdicts.
// A long-lived `gg mcp` server never re-roots, so the cache is the only thing
// standing between it and a stale "feature unavailable".
func TestPreflightReResolvesWhenAnotherProcessChangesAMarker(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	ctx := context.Background()

	vs, err := svc.Preflight(ctx)
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if st := verdictState(t, vs, FeatureVersions); st != preflight.Satisfied {
		t.Fatalf("versions verdict on a fresh repo = %v, want Satisfied", st)
	}

	// Another gg process migrates the versions store to a format this build
	// cannot read. gitRunDir writes the ref exactly as a peer process would —
	// nothing tells this Service about it.
	head, err := svc.Repo().RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	gitRunDir(t, dir, "", "update-ref", git.MetaRef(StoreVersions, 99), head)

	vs, err = svc.Preflight(ctx)
	if err != nil {
		t.Fatalf("Preflight (after the peer migration): %v", err)
	}
	if st := verdictState(t, vs, FeatureVersions); st == preflight.Satisfied {
		t.Fatal("versions verdict still Satisfied after a peer wrote a format-99 marker — the cache was served stale")
	}
	if got := svc.preflightMarks[StoreVersions]; got != 99 {
		t.Errorf("recorded marker = %d, want 99", got)
	}
	if svc.FeatureEnabled(ctx, FeatureVersions) {
		t.Error("FeatureEnabled(versions) = true after the peer migration")
	}
}

// TestPreflightMarkerReReadFailureServesTheCache pins the fail-open rule: a
// transient git error on the validating read must never flip a feature off or
// fail the query. The failure is injected by swapping the repo's runner for a
// FakeRunner that errors on for-each-ref once the verdicts are resolved.
func TestPreflightMarkerReReadFailureServesTheCache(t *testing.T) {
	t.Parallel()
	svc := svcAt(cleanDir(t))
	ctx := context.Background()

	want, err := svc.Preflight(ctx)
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}

	fr := gitexec.NewFakeRunner()
	fr.SetError("git for-each-ref (gg)", errors.New("boom"))
	svc.repo = &git.Repo{Runner: fr}

	got, err := svc.Preflight(ctx)
	if err != nil {
		t.Fatalf("Preflight after a failing marker re-read = %v, want the cached verdicts", err)
	}
	if len(got) != len(want) {
		t.Fatalf("verdicts after a failing re-read = %d, want the cached %d", len(got), len(want))
	}
	for i := range got {
		if got[i].State != want[i].State {
			t.Errorf("verdict %q = %v after a failing re-read, want the cached %v",
				got[i].Feature.ID, got[i].State, want[i].State)
		}
	}
	if !svc.preflightDone {
		t.Error("a failing marker re-read dropped the cache; it must be kept")
	}
	if !svc.FeatureEnabled(ctx, FeatureVersions) {
		t.Error("FeatureEnabled(versions) = false after a transient probe failure")
	}
}

func verdictState(t *testing.T, vs []preflight.Verdict, id string) preflight.State {
	t.Helper()
	for _, v := range vs {
		if v.Feature.ID == id {
			return v.State
		}
	}
	t.Fatalf("no verdict for feature %q", id)
	return preflight.Satisfied
}

func TestFeatureEnabledAndErrFeatureDisabled(t *testing.T) {
	t.Parallel()
	svc := svcAt(cleanDir(t))
	ctx := context.Background()

	if !svc.FeatureEnabled(ctx, FeatureVersions) {
		t.Error("FeatureEnabled(versions) = false on a fresh repo, want true")
	}
	if svc.FeatureEnabled(ctx, "no-such-feature") {
		t.Error("FeatureEnabled of an unknown id = true, want false")
	}

	err := error(&ErrFeatureDisabled{ID: FeatureVersions, Reason: "the versions store is at format 9"})
	var target *ErrFeatureDisabled
	if !errors.As(err, &target) || target.ID != FeatureVersions {
		t.Errorf("errors.As did not unwrap ErrFeatureDisabled: %v", err)
	}
	if got := err.Error(); got == "" {
		t.Error("ErrFeatureDisabled.Error() is empty")
	}
}

func TestPendingMigrationsIsEmptyWhenNothingIsRepairable(t *testing.T) {
	t.Parallel()
	svc := svcAt(cleanDir(t))

	got, err := svc.PendingMigrations(context.Background())
	if err != nil {
		t.Fatalf("PendingMigrations: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("PendingMigrations = %v, want none on a current repo", got)
	}
}

func TestPendingMigrationsListsRepairableFeaturesWithTheirRefs(t *testing.T) {
	t.Parallel()
	svc := svcAt(cleanDir(t))
	ctx := context.Background()

	head, err := svc.Repo().RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	ref := "refs/gg/versions/main/1700000000-rebase"
	if err := svc.Repo().UpdateRef(ctx, ref, head); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	// Stand in for a future build: versions requires format 2 and can repair 1.
	svc.preflightMu.Lock()
	svc.preflightDone = true
	svc.preflightOut = []preflight.Verdict{{
		Feature: preflight.Feature{
			ID:          FeatureVersions,
			Criticality: preflight.Optional,
			Migrate: &preflight.Migration{Store: StoreVersions, From: 1, To: 2,
				Describe: func() preflight.Text {
					return preflight.Text{Format: "discards every recorded branch version in %s", Args: []any{"this repository"}}
				}},
		},
		State:  preflight.Repairable,
		Reason: preflight.Text{Format: "the %s store is at format %d", Args: []any{StoreVersions, 1}},
	}}
	svc.preflightMu.Unlock()

	got, err := svc.PendingMigrations(ctx)
	if err != nil {
		t.Fatalf("PendingMigrations: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("PendingMigrations returned %d entries, want 1", len(got))
	}
	m := got[0]
	if m.Feature != FeatureVersions || m.From != 1 || m.To != 2 {
		t.Errorf("migration = %+v, want versions 1→2", m)
	}
	if m.Consequence == "" {
		t.Error("Consequence is empty; consent has nothing to show")
	}
	if len(m.Refs) != 1 || m.Refs[0] != ref {
		t.Errorf("Refs = %v, want [%s]", m.Refs, ref)
	}

	if err := svc.RunMigration(ctx, m); err != nil {
		t.Fatalf("RunMigration: %v", err)
	}
	left, err := svc.Repo().ForEachRef(ctx, "refs/gg/versions/")
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("%d version refs survived RunMigration, want 0", len(left))
	}
}
