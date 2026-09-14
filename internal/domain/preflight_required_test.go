package domain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/preflight"
)

// TestPreflightRequiredMakesExactlyOneGitInvocation pins the entire
// justification for PreflightRequired existing: core's only requirement is
// GitVersion, which needs no store probes, so a Required-only resolve must
// cost exactly one `git version` call — never a for-each-ref.
func TestPreflightRequiredMakesExactlyOneGitInvocation(t *testing.T) {
	t.Parallel()
	fr := gitexec.NewFakeRunner()
	fr.SetResponse("git version", gitexec.Result{Stdout: "git version 2.40.0\n"})
	svc := New(&git.Repo{Runner: fr})

	if err := svc.PreflightRequired(context.Background()); err != nil {
		t.Fatalf("PreflightRequired with a current git = %v, want nil", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("PreflightRequired made %d git invocations, want exactly 1: %+v", len(fr.Calls), fr.Calls)
	}
	if fr.Calls[0].Name != "git version" {
		t.Errorf("PreflightRequired's one invocation was %q, want %q", fr.Calls[0].Name, "git version")
	}
}

// TestPreflightRequiredTooOldGitNamesTheMinimum covers the refusal case: an
// old git makes core Unsatisfiable, and the error must name the version gg
// needs.
func TestPreflightRequiredTooOldGitNamesTheMinimum(t *testing.T) {
	t.Parallel()
	fr := gitexec.NewFakeRunner()
	fr.SetResponse("git version", gitexec.Result{Stdout: "git version 2.10.0\n"})
	svc := New(&git.Repo{Runner: fr})

	err := svc.PreflightRequired(context.Background())
	if err == nil {
		t.Fatal("PreflightRequired with a too-old git = nil, want an error naming the minimum")
	}
	var rerr *ErrRequiredFeatureUnsatisfiable
	if !errors.As(err, &rerr) {
		t.Fatalf("PreflightRequired error = %T, want *ErrRequiredFeatureUnsatisfiable", err)
	}
	if rerr.ID != FeatureCore {
		t.Errorf("ErrRequiredFeatureUnsatisfiable.ID = %q, want %q", rerr.ID, FeatureCore)
	}
	want := "2.30.0" // MinGitVersion
	if !strings.Contains(err.Error(), want) {
		t.Errorf("PreflightRequired error = %q, want it to name %q", err.Error(), want)
	}
}

// TestPreflightRequiredFailsOpenOnAProbeError matches FeatureEnabled's rule:
// a transient git failure must never keep gg from starting.
func TestPreflightRequiredFailsOpenOnAProbeError(t *testing.T) {
	t.Parallel()
	fr := gitexec.NewFakeRunner()
	fr.SetError("git version", errors.New("boom"))
	svc := New(&git.Repo{Runner: fr})

	if err := svc.PreflightRequired(context.Background()); err != nil {
		t.Errorf("PreflightRequired after a probe error = %v, want nil (fail open)", err)
	}
}

// TestPreflightRequiredDoesNotPoisonTheFullCache is the trap test: a
// Required-only resolve must never populate preflightOut/preflightDone/
// preflightMarks. If it did, a later FeatureEnabled/Preflight call would
// answer from verdicts computed against an empty Probes.Stores, where a
// store with data but no marker reads as "no data" (Satisfied) instead of
// being probed for real — silently opening a gate that should stay shut.
func TestPreflightRequiredDoesNotPoisonTheFullCache(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	cr := newCountingRunner(gitexec.NewExecRunner("git", dir, observ.NewRing(50)))
	svc := New(&git.Repo{Runner: cr})
	ctx := context.Background()

	if err := svc.PreflightRequired(ctx); err != nil {
		t.Fatalf("PreflightRequired: %v", err)
	}
	if n := cr.count("git version"); n != 1 {
		t.Errorf("PreflightRequired ran git version %d times, want 1", n)
	}
	if n := cr.count("git for-each-ref (gg)"); n != 0 {
		t.Errorf("PreflightRequired ran for-each-ref %d times, want 0 — core needs no store probes", n)
	}

	// The trap: the cache must be untouched.
	if svc.preflightDone {
		t.Fatal("preflightDone = true after PreflightRequired; it must never populate the full cache")
	}
	if svc.preflightOut != nil {
		t.Fatal("preflightOut != nil after PreflightRequired; it must never populate the full cache")
	}
	if svc.preflightMarks != nil {
		t.Fatal("preflightMarks != nil after PreflightRequired; it must never populate the full cache")
	}

	// A subsequent full Preflight/FeatureEnabled must still resolve for
	// real — not serve a stale or partial result cached by the call above.
	cr.reset()
	vs, err := svc.Preflight(ctx)
	if err != nil {
		t.Fatalf("Preflight after PreflightRequired: %v", err)
	}
	if n := cr.count("git version"); n != 1 {
		t.Errorf("Preflight after PreflightRequired ran git version %d times, want 1 (a real resolve, not a cache hit)", n)
	}
	if n := cr.count("git for-each-ref (gg)"); n != 2 {
		t.Errorf("Preflight after PreflightRequired ran for-each-ref %d times, want 2 (markers + version refs)", n)
	}
	if st := verdictState(t, vs, FeatureVersions); st != preflight.Satisfied {
		t.Errorf("versions verdict on a fresh repo after PreflightRequired = %v, want Satisfied", st)
	}
	if !svc.FeatureEnabled(ctx, FeatureVersions) {
		t.Error("FeatureEnabled(versions) = false after PreflightRequired then Preflight, want true on a fresh repo")
	}
	if !svc.preflightDone {
		t.Error("preflightDone = false after a real Preflight call; it should now be cached")
	}
}
