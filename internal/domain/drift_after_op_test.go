package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/repogate"
)

// TestDriftAfterReportsDriftOnNewestVersion fabricates a version snapshot for
// "feat" recorded at oursSha, then adds a further commit to feat so the
// branch's tip now carries more than the recorded version did. DriftAfter
// must resolve the newest version ref (BranchVersions is newest-first) and
// the branch's current tip on its own, then report the drift — mirroring
// what a frontend gets from one call after an op returns.
func TestDriftAfterReportsDriftOnNewestVersion(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t) // main, base commit, f.txt = "hi\n"
	svc := svcAt(dir)
	ctx := context.Background()

	baseSha := gitOutDir(t, dir, "rev-parse", "main")

	gitRunDir(t, dir, "", "checkout", "-q", "-b", "feat")
	writeFile(t, dir, "feat.txt", "feat\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "feat: add feat.txt")
	oursSha := gitOutDir(t, dir, "rev-parse", "feat")

	const unix = int64(1700004000)
	meta := git.VersionMeta{Op: "snapshot", Ours: oursSha, Other: baseSha, Base: baseSha, Source: "feat", Target: "feat"}
	syn, err := svc.Repo().WriteVersionSnapshot(ctx, baseSha, meta, unix)
	if err != nil {
		t.Fatalf("WriteVersionSnapshot: %v", err)
	}
	ref := git.VersionRef("feat", "snapshot", unix)
	if err := svc.Repo().UpdateRef(ctx, ref, syn); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}
	stampVersionsFormat(t, dir)

	// The branch keeps moving after the version was recorded.
	writeFile(t, dir, "feat2.txt", "feat2\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "feat: add feat2.txt")

	got, err := svc.DriftAfter(ctx, "feat")
	if err != nil {
		t.Fatalf("DriftAfter: %v", err)
	}
	if got.Ref != ref {
		t.Errorf("Ref = %q, want %q (the newest version)", got.Ref, ref)
	}
	if !got.Checked {
		t.Fatalf("Checked = false, want true: %+v", got)
	}
	if !got.Report.Drifted() {
		t.Fatalf("Drifted() = false, want true (feat2.txt was added after the version was recorded): %+v", got.Report)
	}
}

// TestDriftAfterNoVersionsIsUncheckedNoError covers a branch that has never
// had a version recorded: nothing to compare against, so DriftAfter must
// report Checked == false and return no error rather than treating an empty
// history as a failure.
func TestDriftAfterNoVersionsIsUncheckedNoError(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	ctx := context.Background()
	stampVersionsFormat(t, dir) // versions feature ON, but nothing recorded for "main"

	got, err := svc.DriftAfter(ctx, "main")
	if err != nil {
		t.Fatalf("DriftAfter: %v", err)
	}
	if got.Checked {
		t.Fatalf("Checked = true, want false for a branch with no recorded versions: %+v", got)
	}
}

// TestDriftAfterFeatureDisabledIsEmptyNoError covers a repository where the
// versions feature preflight is OFF. A version ref exists on disk (as legacy
// format-1 data would), but stampVersionsFormat is deliberately NOT called —
// per its doc comment, an unmarked store resolves as format 1 and the
// versions feature gates BranchVersions with *ErrFeatureDisabled. DriftAfter
// must unwrap that via errors.As and report it as "nothing recorded", not as
// a failure: the feature being off means nothing was recorded, so there is
// nothing to say.
func TestDriftAfterFeatureDisabledIsEmptyNoError(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	ctx := context.Background()

	mainSha := gitOutDir(t, dir, "rev-parse", "main")
	ref := git.VersionRef("main", "snapshot", 1700006000)
	gitRunDir(t, dir, "", "update-ref", ref, mainSha)
	// No stampVersionsFormat: the store stays at the unmarked (format-1)
	// state, so the versions feature preflight resolves OFF.

	got, err := svc.DriftAfter(ctx, "main")
	if err != nil {
		t.Fatalf("DriftAfter: %v, want no error when the feature is disabled", err)
	}
	if got.Ref != "" || got.Checked || len(got.Report.Added) != 0 || len(got.Report.Removed) != 0 {
		t.Fatalf("DriftAfter = %+v, want a zero DriftReport when the feature is disabled", got)
	}
}

// probeRunner wraps a real gitexec.Runner and calls onRun (if set) for every
// invocation whose label matches watchLabel, right before delegating —
// letting a test observe service-internal state (here, the repo gate's
// queue) at the exact moment a specific git subprocess is about to run.
type probeRunner struct {
	gitexec.Runner
	watchLabel string
	onRun      func()
}

func (p *probeRunner) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	if name == p.watchLabel && p.onRun != nil {
		p.onRun()
	}
	return p.Runner.Run(ctx, name, argv)
}

// TestDriftAfterDiffsRunWithNoGateReservationHeld pins the lock-ordering
// contract from spec 1's regression (preflight probes running under an
// exclusive TreeWrite hold): DriftAfter's two name-status diffs — the actual
// git work DriftSince does — must run with NO repo-gate reservation held at
// all, by anyone, at that moment. BranchVersions legitimately takes its own
// brief Read reservation earlier in the call (a normal domain read), but it
// must be released before DriftSince's diffs start; DriftAfter itself must
// never wrap the diffs in a reservation of its own.
//
// This is asserted the way TestExecuteResolvesPreflightBeforeAcquiringTheGate
// asserts its ordering: a probe planted on the git command in question reads
// the gate's live queue at the instant that command is about to run. Here
// the queue must come back completely empty — if a future edit moved the
// diffs inside BranchVersions' query() closure (extending its Read
// reservation over them, mirroring spec 1's bug), this probe would see that
// reservation still held and fail.
func TestDriftAfterDiffsRunWithNoGateReservationHeld(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	ctx := context.Background()

	baseSha := gitOutDir(t, dir, "rev-parse", "main")
	gitRunDir(t, dir, "", "checkout", "-q", "-b", "feat")
	writeFile(t, dir, "feat.txt", "feat\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "feat: add feat.txt")
	oursSha := gitOutDir(t, dir, "rev-parse", "feat")

	var svc *Service // assigned below; the probe closure resolves it lazily,
	// since it only fires once DriftAfter actually runs a diff.
	real := gitexec.NewExecRunner("git", dir, observ.NewRing(50))
	var queues [][]repogate.Entry
	runner := &probeRunner{
		Runner:     real,
		watchLabel: "git diff --name-status",
		onRun: func() {
			queues = append(queues, svc.gateFor(ctx).Queue())
		},
	}
	svc = New(&git.Repo{Runner: runner})

	const unix = int64(1700005000)
	meta := git.VersionMeta{Op: "snapshot", Ours: oursSha, Other: baseSha, Base: baseSha, Source: "feat", Target: "feat"}
	syn, err := svc.Repo().WriteVersionSnapshot(ctx, baseSha, meta, unix)
	if err != nil {
		t.Fatalf("WriteVersionSnapshot: %v", err)
	}
	ref := git.VersionRef("feat", "snapshot", unix)
	if err := svc.Repo().UpdateRef(ctx, ref, syn); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}
	stampVersionsFormat(t, dir)

	got, err := svc.DriftAfter(ctx, "feat")
	if err != nil {
		t.Fatalf("DriftAfter: %v", err)
	}
	if got.Ref != ref || !got.Checked {
		t.Fatalf("DriftAfter result = %+v, want a checked report against %q", got, ref)
	}

	if len(queues) == 0 {
		t.Fatalf("the %q probe never fired — the test fixture did not reach a diff, so the assertion below is vacuous", runner.watchLabel)
	}
	for i, q := range queues {
		if len(q) != 0 {
			t.Errorf("gate queue during diff #%d = %+v, want empty — a reservation was held while the name-status diff ran", i, q)
		}
	}
}
