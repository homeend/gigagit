package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/changeset"
	"github.com/homeend/gigagit/internal/git"
)

// TestDriftSinceReportsResurrectionAfterRebase is the motivating case: main
// has f3.txt; branch feat modifies it and adds f4.txt; main then deletes
// f3.txt. Rebasing feat onto main hits a modify/delete conflict on f3.txt,
// resolved by KEEPING the file (git add f3.txt instead of git rm). The
// result must read as feat ADDING f3.txt — a file it never added — because
// from main's post-delete point of view, the file came back.
func TestDriftSinceReportsResurrectionAfterRebase(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t) // main, base commit, f.txt = "hi\n"
	svc := svcAt(dir)
	ctx := context.Background()

	writeFile(t, dir, "f3.txt", "hi\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "main: add f3.txt")
	baseSha := gitOutDir(t, dir, "rev-parse", "main")

	gitRunDir(t, dir, "", "checkout", "-q", "-b", "feat")
	writeFile(t, dir, "f3.txt", "changed\n")
	writeFile(t, dir, "f4.txt", "hi\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "feat: modify f3, add f4")
	oursSha := gitOutDir(t, dir, "rev-parse", "feat")

	gitRunDir(t, dir, "", "checkout", "-q", "main")
	gitRunDir(t, dir, "", "rm", "-q", "f3.txt")
	gitRunDir(t, dir, "", "commit", "-qm", "main: delete f3")
	otherSha := gitOutDir(t, dir, "rev-parse", "main")

	const unix = int64(1700001000)
	meta := git.VersionMeta{Op: "rebase", Ours: oursSha, Other: otherSha, Base: baseSha, Source: "feat", Target: "main"}
	syn, err := svc.Repo().WriteVersionSnapshot(ctx, otherSha, meta, unix)
	if err != nil {
		t.Fatalf("WriteVersionSnapshot: %v", err)
	}
	ref := git.VersionRef("feat", "rebase", unix)
	if err := svc.Repo().UpdateRef(ctx, ref, syn); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}
	stampVersionsFormat(t, dir)

	// Rebase feat onto main; resolve the modify/delete conflict on f3.txt by
	// KEEPING the file (git add, not git rm).
	gitRunDir(t, dir, "", "checkout", "-q", "feat")
	// tolerate="rebase" only compares args[0]; it can't distinguish the
	// expected modify/delete conflict from some other rebase failure. The
	// assertions below on newFeatTip's content and DriftSince's result would
	// still catch a materially different outcome, so this is left as-is
	// rather than reworking the shared gitRunDir helper.
	gitRunDir(t, dir, "rebase", "rebase", "main") // tolerates the expected modify/delete conflict
	gitRunDir(t, dir, "", "add", "f3.txt")        // resolve by KEEPING the file
	// -c core.editor=true pins the editor: `rebase --continue` after a
	// modify/delete resolution commits the replayed change and launches
	// $GIT_EDITOR to confirm the message, which would hang or fail on a
	// machine without one (gitRunDir forwards os.Environ() unchanged).
	gitRunDir(t, dir, "", "-c", "core.editor=true", "rebase", "--continue")
	newFeatTip := gitOutDir(t, dir, "rev-parse", "feat")

	got, err := svc.DriftSince(ctx, ref, newFeatTip)
	if err != nil {
		t.Fatalf("DriftSince: %v", err)
	}
	if !got.Checked {
		t.Fatalf("Checked = false, want true: %+v", got)
	}
	wantAdded := []changeset.Entry{{Status: 'A', Path: "f3.txt"}}
	if len(got.Report.Added) != 1 || got.Report.Added[0] != wantAdded[0] {
		t.Fatalf("Added = %+v, want %+v", got.Report.Added, wantAdded)
	}
	// The old M f3.txt entry cancels out via the status-change path (M -> A
	// counts as an Add, not an Add+Remove pair; see changeset.Compare), so
	// Removed carries the superseded M f3.txt record.
	wantRemoved := []changeset.Entry{{Status: 'M', Path: "f3.txt"}}
	if len(got.Report.Removed) != 1 || got.Report.Removed[0] != wantRemoved[0] {
		t.Fatalf("Removed = %+v, want %+v", got.Report.Removed, wantRemoved)
	}
	if !got.Report.Drifted() {
		t.Fatalf("Drifted() = false, want true: %+v", got.Report)
	}
}

// TestDriftSinceIgnoresUpstreamChangesAfterPullMerge is the regression guard
// for the wrong formula. feat forks from main, adds feat.txt (its own,
// unrelated change). Upstream (main) independently adds upstream.txt — a
// file feat never touched. feat then pulls (merges) main in, producing a
// clean merge commit M whose first parent IS feat's own old tip.
//
// The correct comparison (Other..newTip, i.e. U..M) sees only feat.txt on
// both sides and must NOT drift. The tempting oldTip..newTip (B_old..M)
// would show upstream.txt as newly added and falsely alarm — if DriftSince
// is ever changed to use that range, this test must fail.
func TestDriftSinceIgnoresUpstreamChangesAfterPullMerge(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t) // main, base commit, f.txt = "hi\n"
	svc := svcAt(dir)
	ctx := context.Background()

	baseSha := gitOutDir(t, dir, "rev-parse", "main")

	gitRunDir(t, dir, "", "checkout", "-q", "-b", "feat")
	writeFile(t, dir, "feat.txt", "feat\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "feat: add feat.txt")
	oldTip := gitOutDir(t, dir, "rev-parse", "feat")

	gitRunDir(t, dir, "", "checkout", "-q", "main")
	writeFile(t, dir, "upstream.txt", "up\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "main: add upstream.txt")
	upstreamTip := gitOutDir(t, dir, "rev-parse", "main")

	gitRunDir(t, dir, "", "checkout", "-q", "feat")
	gitRunDir(t, dir, "", "merge", "-q", "--no-edit", "main")
	mergeSha := gitOutDir(t, dir, "rev-parse", "feat")

	const unix = int64(1700002000)
	meta := git.VersionMeta{Op: "merge", Ours: oldTip, Other: upstreamTip, Base: baseSha, Source: "main", Target: "feat"}
	syn, err := svc.Repo().WriteVersionSnapshot(ctx, upstreamTip, meta, unix)
	if err != nil {
		t.Fatalf("WriteVersionSnapshot: %v", err)
	}
	ref := git.VersionRef("feat", "merge", unix)
	if err := svc.Repo().UpdateRef(ctx, ref, syn); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}
	stampVersionsFormat(t, dir)

	got, err := svc.DriftSince(ctx, ref, mergeSha)
	if err != nil {
		t.Fatalf("DriftSince: %v", err)
	}
	if !got.Checked {
		t.Fatalf("Checked = false, want true: %+v", got)
	}
	if got.Report.Drifted() {
		t.Fatalf("Drifted() = true, want false (upstream's own files must not alarm): %+v", got.Report)
	}
}

// TestDriftSinceSkipsEmptyBeforeSet covers a fast-forward pull (Base ==
// Ours) and, more generally, a branch with nothing ahead: DriftSince must
// return Checked == false and no findings, with no op-path special-casing.
func TestDriftSinceSkipsEmptyBeforeSet(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	ctx := context.Background()

	tip := gitOutDir(t, dir, "rev-parse", "main")

	const unix = int64(1700003000)
	// Base == Ours: the branch contributed nothing before the op (the
	// fast-forward case). Other is set to tip too — it just needs to be
	// non-empty so the "nothing recorded" short-circuit doesn't mask the
	// empty-before-set path under test; it is never dereferenced by git,
	// since the empty-before-set return happens before the after-diff.
	meta := git.VersionMeta{Op: "pull", Ours: tip, Other: tip, Base: tip, Source: "origin/main", Target: "main"}
	syn, err := svc.Repo().WriteVersionSnapshot(ctx, tip, meta, unix)
	if err != nil {
		t.Fatalf("WriteVersionSnapshot: %v", err)
	}
	ref := git.VersionRef("main", "pull", unix)
	if err := svc.Repo().UpdateRef(ctx, ref, syn); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}
	stampVersionsFormat(t, dir)

	got, err := svc.DriftSince(ctx, ref, tip)
	if err != nil {
		t.Fatalf("DriftSince: %v", err)
	}
	if got.Checked {
		t.Fatalf("Checked = true, want false for an empty before-set: %+v", got)
	}
	if len(got.Report.Added) != 0 || len(got.Report.Removed) != 0 {
		t.Fatalf("Report = %+v, want empty", got.Report)
	}
}
