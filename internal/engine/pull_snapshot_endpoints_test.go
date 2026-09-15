package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

// rev is revAt without the trailing newline, for comparing against the shas
// a version record stores.
func rev(t *testing.T, dir, ref string) string {
	t.Helper()
	return strings.TrimSpace(revAt(t, dir, ref))
}

func pullDeps(repo GitOps, dec Decider) OpDeps {
	d := enabledDeps(repo)
	d.Decider = dec
	return d
}

// TestSmartPullFastForwardRecordsPostFetchEndpoints is the fast-forward half
// of the pull-site endpoint guard. Two things are asserted at once, because
// they are the same bug from two sides:
//
//  1. A fast-forward pull RECORDS a version. It used to record nothing, which
//     left DriftAfter — which always compares against the branch's NEWEST
//     record — comparing the branch against some older rebase/merge snapshot
//     and reporting the commits this pull just brought down as drift.
//  2. Other is the POST-fetch remote tip, not the stale one the clone had
//     before. A snapshot taken before the fetch freezes the previous fetch's
//     origin/<branch>, and the after-side diff then reports the whole pull as
//     the branch's own contribution.
//
// A fast-forward records Base == Ours (the branch contributed nothing on top
// of the upstream tip), which is exactly what makes DriftSince's empty-before
// skip keep it silent.
func TestSmartPullFastForwardRecordsPostFetchEndpoints(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	seed := filepath.Join(filepath.Dir(clone), "seed")

	preOpTip := rev(t, clone, "main")
	staleOther := rev(t, clone, "origin/main")
	freshOther := rev(t, seed, "HEAD")
	if staleOther == freshOther {
		t.Fatalf("fixture is not behind origin: %s", staleOther)
	}

	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(), pullDeps(repo, MapDecider{}))
	if err != nil {
		t.Fatalf("smart pull: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}

	bv := findVersionBV(t, repo, "main", "pull")
	if bv.Hash != preOpTip {
		t.Errorf("Hash = %s, want the pre-op tip %s", bv.Hash, preOpTip)
	}
	if bv.Ours != preOpTip {
		t.Errorf("Ours = %s, want the pre-op tip %s", bv.Ours, preOpTip)
	}
	if bv.Other == staleOther {
		t.Errorf("Other = %s — the PRE-fetch origin/main; the snapshot must run after the fetch", bv.Other)
	}
	if bv.Other != freshOther {
		t.Errorf("Other = %s, want the post-fetch origin/main %s", bv.Other, freshOther)
	}
	if bv.Base != bv.Ours {
		t.Errorf("Base = %s, want Base == Ours (%s) for a fast-forward — the empty-before set is what keeps it silent", bv.Base, bv.Ours)
	}
	if bv.Target != "origin/main" {
		t.Errorf("Target = %q, want the NAME origin/main (only Other is resolved)", bv.Target)
	}
}

// TestSmartPullDivergedRebaseRecordsForkPointBase covers the diverged half:
// answering the non-fast-forward fork rebases, and the record must carry the
// branch's own pre-op tip as Ours, the post-fetch remote tip as Other, and
// their FORK POINT as Base — the one thing that cannot be recomputed once the
// rebase has replayed the local commit on top of the remote tip.
func TestSmartPullDivergedRebaseRecordsForkPointBase(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	seed := filepath.Join(filepath.Dir(clone), "seed")
	os.WriteFile(filepath.Join(clone, "local.txt"), []byte("local\n"), 0o644)
	gitAt(t, clone, "add", ".")
	gitAt(t, clone, "commit", "-m", "local")

	preOpTip := rev(t, clone, "main")
	forkPoint := rev(t, clone, "main~1")
	freshOther := rev(t, seed, "HEAD")

	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(),
		pullDeps(repo, MapDecider{"non-fast-forward": "rebase"}))
	if err != nil {
		t.Fatalf("smart pull (rebase): %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}

	bv := findVersionBV(t, repo, "main", "pull")
	if bv.Ours != preOpTip {
		t.Errorf("Ours = %s, want the pre-rebase tip %s", bv.Ours, preOpTip)
	}
	if bv.Other != freshOther {
		t.Errorf("Other = %s, want the post-fetch origin/main %s", bv.Other, freshOther)
	}
	if bv.Base != forkPoint {
		t.Errorf("Base = %s, want the fork point %s", bv.Base, forkPoint)
	}
	if rev(t, clone, "main") == preOpTip {
		t.Fatal("main did not move: the fixture never exercised the rebase path")
	}
}

// TestSmartPullBackgroundFastForwardRecordsPostFetchOther is the third armed
// pull path: PullInBackground's FastForwardRef. The record is written AFTER
// the branch has moved, so this pins both halves of that inversion — Ours and
// the snapshotted tip are the state the fast-forward replaced, and Other is
// the tip it landed on, read from the moved branch itself rather than from
// refs/remotes/<remote>/<branch>, which an explicit refspec updates only
// opportunistically.
func TestSmartPullBackgroundFastForwardRecordsPostFetchOther(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	root := filepath.Dir(clone)
	seed := filepath.Join(root, "seed")

	// A non-current branch, behind origin, that a background pull can move.
	gitAt(t, seed, "checkout", "-b", "dev")
	gitAt(t, seed, "push", "-u", "origin", "dev")
	gitAt(t, clone, "fetch", "origin")
	gitAt(t, clone, "branch", "dev", "origin/dev")
	staleOther := rev(t, clone, "origin/dev")

	os.WriteFile(filepath.Join(seed, "d.txt"), []byte("d\n"), 0o644)
	gitAt(t, seed, "add", ".")
	gitAt(t, seed, "commit", "-m", "dev work")
	gitAt(t, seed, "push", "origin", "dev")
	freshOther := rev(t, seed, "HEAD")

	preOpTip := rev(t, clone, "dev")
	res, err := SmartPull{Branch: "dev", Intent: PullInBackground}.Run(context.Background(),
		pullDeps(repo, MapDecider{}))
	if err != nil {
		t.Fatalf("background pull: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}

	bv := findVersionBV(t, repo, "dev", "pull")
	if bv.Hash != preOpTip || bv.Ours != preOpTip {
		t.Errorf("Hash/Ours = %s/%s, want the pre-op tip %s", bv.Hash, bv.Ours, preOpTip)
	}
	if bv.Other == staleOther {
		t.Errorf("Other = %s — the PRE-fetch origin/dev; FastForwardRef's own refspec fetch is not a substitute", bv.Other)
	}
	if bv.Other != freshOther {
		t.Errorf("Other = %s, want the post-fetch origin/dev %s", bv.Other, freshOther)
	}
}

// TestSnapshotResolvesOtherSoARenamedRefCannotMoveIt is the I2 guard: Other
// is stored as a RESOLVED sha, never as the name the call site passed.
// Callers pass whatever names the second endpoint — `op.Onto` for rebase
// (which can be a revision like HEAD~3), "<remote>/<branch>" for pull — and a
// name re-resolves at diff time, long after the op moved what it pointed at.
func TestSnapshotResolvesOtherSoARenamedRefCannotMoveIt(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()

	if err := repo.CreateBranch(ctx, "feat", ""); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "m.txt"), []byte("m\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-qm", "main work")
	mainAtSnapshot := rev(t, dir, "main")

	oursSha, err := repo.RevParse(ctx, "feat")
	if err != nil {
		t.Fatal(err)
	}
	// other is passed as a NAME, the way smart_rebase.go passes op.Onto.
	snapshotBranchTip(ctx, enabledDeps(repo), "feat", "rebase", oursSha, "main")

	// The named ref moves — a later commit, a fetch, the rebase itself.
	os.WriteFile(filepath.Join(dir, "m2.txt"), []byte("m2\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-qm", "main moves on")
	if rev(t, dir, "main") == mainAtSnapshot {
		t.Fatal("fixture did not move main")
	}

	bv := findVersionBV(t, repo, "feat", "rebase")
	if bv.Other != mainAtSnapshot {
		t.Errorf("Other = %s, want the sha main had AT SNAPSHOT TIME %s — a frozen record must freeze both endpoints", bv.Other, mainAtSnapshot)
	}
	if bv.Other == "main" {
		t.Error("Other was stored as a ref NAME; it re-resolves at diff time and the after-side base moves")
	}
	if bv.Target != "main" {
		t.Errorf("Target = %q, want the name main — only Other is resolved", bv.Target)
	}
}

// countingRunner delegates to a real Runner while tallying how many git
// invocations were fetches. The pull fixtures drive real git, so the only way
// to count round-trips is to watch the argv going past.
type countingRunner struct {
	gitexec.Runner
	mu      sync.Mutex
	fetches int
}

func (c *countingRunner) count(argv []string) {
	for _, a := range argv {
		if a == "fetch" {
			c.mu.Lock()
			c.fetches++
			c.mu.Unlock()
			return
		}
		// Only the subcommand slot counts; a later "fetch" is a ref name.
		if !strings.HasPrefix(a, "-") {
			return
		}
	}
}

func (c *countingRunner) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	c.count(argv)
	return c.Runner.Run(ctx, name, argv)
}

func (c *countingRunner) RunEnv(ctx context.Context, name string, argv, env []string) (gitexec.Result, error) {
	c.count(argv)
	return c.Runner.RunEnv(ctx, name, argv, env)
}

func (c *countingRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (gitexec.Result, error) {
	c.count(argv)
	return c.Runner.Stream(ctx, name, argv, onLine)
}

func (c *countingRunner) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fetches
}

// TestSmartPullBackgroundFastForwardFetchesOnce pins the round-trip budget of
// the background fast-forward. FastForwardRef IS a fetch, so the plain fetch
// that used to precede it — added only to refresh the remote-tracking ref the
// snapshot read as Other — doubled the network cost of every background pull.
// On the repos gg targets that is the expensive half of the operation, so the
// count is a guarded invariant, not an incidental property.
func TestSmartPullBackgroundFastForwardFetchesOnce(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	root := filepath.Dir(clone)
	seed := filepath.Join(root, "seed")

	gitAt(t, seed, "checkout", "-b", "dev")
	gitAt(t, seed, "push", "-u", "origin", "dev")
	gitAt(t, clone, "fetch", "origin")
	gitAt(t, clone, "branch", "dev", "origin/dev")

	os.WriteFile(filepath.Join(seed, "d.txt"), []byte("d\n"), 0o644)
	gitAt(t, seed, "add", ".")
	gitAt(t, seed, "commit", "-m", "dev work")
	gitAt(t, seed, "push", "origin", "dev")

	counter := &countingRunner{Runner: repo.Runner}
	repo.Runner = counter

	if _, err := (SmartPull{Branch: "dev", Intent: PullInBackground}).Run(context.Background(),
		pullDeps(repo, MapDecider{})); err != nil {
		t.Fatalf("background pull: %v", err)
	}
	if got := counter.total(); got != 1 {
		t.Errorf("fetch invocations = %d, want exactly 1 (FastForwardRef is itself the fetch)", got)
	}
}

// pullVersionRefs lists branch's recorded pull versions.
func pullVersionRefs(t *testing.T, repo *git.Repo, branch string) []string {
	t.Helper()
	infos, err := repo.ForEachRef(context.Background(), git.VersionRefPrefix+branch)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, i := range infos {
		if _, op, _, ok := git.ParseVersionRef(i.Ref); ok && op == "pull" {
			out = append(out, i.Ref)
		}
	}
	return out
}

// TestSmartPullAbortRecordsNothing is the first half of the no-op guard: a
// pull the user aborts at the diverged fork moved nothing, so there is
// nothing to record. The snapshot used to be written unconditionally right
// after the fetch, before the fork was even offered, so declining still left
// a version ref behind.
//
// Safe because all three frontends gate their drift check on Result.Changed,
// which an abort leaves false — an unrecorded abort can never leave a stale
// comparison armed.
func TestSmartPullAbortRecordsNothing(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	os.WriteFile(filepath.Join(clone, "local.txt"), []byte("local\n"), 0o644)
	gitAt(t, clone, "add", ".")
	gitAt(t, clone, "commit", "-m", "local")
	preOpTip := rev(t, clone, "main")

	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(),
		pullDeps(repo, MapDecider{"non-fast-forward": "abort"}))
	if err != nil {
		t.Fatalf("smart pull (abort): %v", err)
	}
	if res.Changed {
		t.Fatalf("result = %+v, want Changed false for an abort", res)
	}
	if rev(t, clone, "main") != preOpTip {
		t.Fatal("fixture moved the branch: the abort path was not exercised")
	}
	if got := pullVersionRefs(t, repo, "main"); len(got) != 0 {
		t.Errorf("aborted pull recorded %v, want nothing", got)
	}
}

// TestSmartPullAlreadyUpToDateRecordsOnce is the second half: repeated pulls
// of a branch that is already current record ONE version between them, not
// one each. The first is a genuine record (the branch had none); every repeat
// after it is byte-identical and is deduplicated. Under the background
// auto-pull lane this is the difference between one ref and one ref per poll.
func TestSmartPullAlreadyUpToDateRecordsOnce(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	deps := pullDeps(repo, MapDecider{})

	// First pull lands the two commits the fixture is behind by.
	if _, err := (SmartPull{Intent: PullAndStay}).Run(context.Background(), deps); err != nil {
		t.Fatalf("first pull: %v", err)
	}
	afterFirst := pullVersionRefs(t, repo, "main")
	if len(afterFirst) != 1 {
		t.Fatalf("after the real pull: %v, want 1", afterFirst)
	}
	tip := rev(t, clone, "main")

	// Three more pulls with nothing to fetch: all no-ops.
	for i := 0; i < 3; i++ {
		if _, err := (SmartPull{Intent: PullAndStay}).Run(context.Background(), deps); err != nil {
			t.Fatalf("no-op pull %d: %v", i, err)
		}
	}
	if rev(t, clone, "main") != tip {
		t.Fatal("a no-op pull moved the branch: fixture is wrong")
	}
	got := pullVersionRefs(t, repo, "main")
	if len(got) != 2 {
		t.Errorf("refs = %v, want 2 (the real pull, plus ONE for the first no-op) — repeats must dedupe", got)
	}
}

// TestSmartPullBackgroundNoOpRecordsOnce is the same guard on the background
// fast-forward path, the one the auto-pull lane actually drives.
func TestSmartPullBackgroundNoOpRecordsOnce(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	root := filepath.Dir(clone)
	seed := filepath.Join(root, "seed")

	gitAt(t, seed, "checkout", "-b", "dev")
	gitAt(t, seed, "push", "-u", "origin", "dev")
	gitAt(t, clone, "fetch", "origin")
	gitAt(t, clone, "branch", "dev", "origin/dev")

	deps := pullDeps(repo, MapDecider{})
	for i := 0; i < 4; i++ {
		if _, err := (SmartPull{Branch: "dev", Intent: PullInBackground}).Run(context.Background(), deps); err != nil {
			t.Fatalf("background pull %d: %v", i, err)
		}
	}
	if got := pullVersionRefs(t, repo, "dev"); len(got) != 1 {
		t.Errorf("refs = %v, want 1 — four polls of a quiet branch must not leave four refs", got)
	}
}
