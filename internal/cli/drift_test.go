package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildResurrectionFixture reproduces Task 8's motivating case in dir (built
// by newRepoDir: "main" checked out with one commit): main gets f3.txt,
// branch "source" modifies it and adds f4.txt, main then deletes f3.txt.
// landOn's ref is then force-moved onto a crafted post-op tip that KEEPS
// f3.txt — the classic resurrection (relative to "other", f3.txt reads as
// status A, not M: a modify/delete conflict resolved by keeping the file).
//
// Driving that resolution through a REAL conflicting `gg rebase`/`gg merge`
// is not possible from the CLI alone: a kept conflict pauses the op
// (detached HEAD, exit non-zero) and the CLI has no `--continue` verb to
// finish it (see the task-10 controller ruling) — resolving one would
// require raw git outside gg, at which point no gg invocation is left to
// observe the completion. So this fixture crafts the already-resolved tip
// directly with raw git and force-moves landOn's ref onto it, leaving the
// actual gg command under test (a real, successful, non-conflicting
// rebase/merge onto "other", which the tip already descends from) to
// exercise the CLI's own success path end to end.
//
// Returns the four endpoints DriftSince needs: base (the merge base),
// ours (source's pre-op tip: f3.txt modified, f4.txt added), other (main's
// tip after deleting f3.txt — what the op lands against), and newTip
// (landOn's tip after the "op": f3.txt and f4.txt both present). Leaves dir
// on branch landOn at newTip, working tree clean.
func buildResurrectionFixture(t *testing.T, dir, source, landOn string) (base, ours, other, newTip string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "f3.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "f3.txt")
	runGit(t, dir, "commit", "-m", "add f3.txt")
	base = runGit(t, dir, "rev-parse", "HEAD")

	runGit(t, dir, "checkout", "-b", source)
	if err := os.WriteFile(filepath.Join(dir, "f3.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f4.txt"), []byte("f4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", source+": modify f3, add f4")
	ours = runGit(t, dir, "rev-parse", "HEAD")

	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "rm", "f3.txt")
	runGit(t, dir, "commit", "-m", "main: delete f3.txt")
	other = runGit(t, dir, "rev-parse", "HEAD")

	runGit(t, dir, "checkout", "--detach", other)
	runGit(t, dir, "checkout", ours, "--", "f3.txt", "f4.txt")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "resolved: kept f3.txt (resurrection)")
	newTip = runGit(t, dir, "rev-parse", "HEAD")

	runGit(t, dir, "branch", "-f", landOn, newTip)
	runGit(t, dir, "checkout", landOn)
	return base, ours, other, newTip
}

// fabricateVersion writes a version snapshot ref for branch directly — raw
// commit-tree + update-ref mirroring internal/git's WriteVersionSnapshot
// shape (internal/cli cannot import internal/git; see stampVersionsFormat's
// doc comment in versions_test.go for the same constraint). unix is chosen
// far in the future so the ref stays BranchVersions' newest (sorted
// descending by Unix) even after the real op under test writes its OWN
// pre-op snapshot with a genuine "now" timestamp — deterministically, with
// no reliance on timing.
func fabricateVersion(t *testing.T, dir, branch, op string, unix int64, base, ours, other string) string {
	t.Helper()
	meta := strings.Join([]string{op, ours, other, base, branch, "onto"}, " ")
	body := fmt.Sprintf("gg version snapshot (%s)\n\nGg-Meta: %s\n", op, meta)
	syn := runGit(t, dir, "commit-tree", ours+"^{tree}", "-p", ours, "-m", body)
	ref := fmt.Sprintf("refs/gg/versions/%s/%d-%s", branch, unix, op)
	runGit(t, dir, "update-ref", ref, syn)
	return ref
}

// TestCmdRebaseReportsChangeSetDrift drives a real, successful `gg rebase`
// (branch "feat", already sitting on the resurrected tip built above, onto
// "other" — its own direct parent, so the rebase is a clean no-op) against
// a fabricated newest version recording feat's pre-resurrection change set.
// cmdRebase must call DriftAfter after the successful op and print the
// resurrected path (f3.txt, which changed status M -> A) headed by a
// change-set-changed statement, with the modified-away entry reported
// quietly as absorbed upstream — and say nothing about f4.txt, which was
// added on both sides and so did not drift.
func TestCmdRebaseReportsChangeSetDrift(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	base, ours, other, _ := buildResurrectionFixture(t, dir, "feat", "feat")
	fabricateVersion(t, dir, "feat", "rebase", 9999999999, base, ours, other)
	stampVersionsFormat(t, dir)

	code, out, errb := runCLI(t, dir, "rebase", "--branch", "feat", other)
	if code != 0 {
		t.Fatalf("rebase exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "feat's change set") {
		t.Fatalf("stdout missing the change-set-changed statement for feat:\n%s", out)
	}
	if !strings.Contains(out, "A f3.txt") {
		t.Fatalf("stdout missing the resurrected path f3.txt as Added:\n%s", out)
	}
	if !strings.Contains(out, "absorbed upstream: M f3.txt") {
		t.Fatalf("stdout missing the quiet Removed line for f3.txt:\n%s", out)
	}
	if strings.Contains(out, "f4.txt") {
		t.Fatalf("stdout alarmed on f4.txt, which did not drift (added on both sides):\n%s", out)
	}
}

// TestCmdRebaseNoDriftIsSilent covers the ordinary case with NO fabrication:
// two branches genuinely diverge and are cleanly (no conflict) rebased.
// A clean automatic replay does not change which paths a branch
// contributes, so DriftAfter's own pre-op snapshot (written by the rebase
// itself) must show Drifted() == false, and cmdRebase must print nothing
// beyond the usual "✓ rebased …" summary.
func TestCmdRebaseNoDriftIsSilent(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	runGit(t, dir, "checkout", "-b", "feat")
	if err := os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "feat: add feat.txt")
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "main: add other.txt")

	code, out, errb := runCLI(t, dir, "rebase", "--branch", "feat", "main")
	if code != 0 {
		t.Fatalf("rebase exit %d: %s", code, errb)
	}
	if strings.Contains(out, "change set") {
		t.Fatalf("expected no drift summary for a clean rebase, got:\n%s", out)
	}
}

// TestCmdMergeReportsChangeSetDrift is TestCmdRebaseReportsChangeSetDrift's
// counterpart for `gg merge`: the resurrected tip lands on "main" (the
// merge TARGET, resolved from the current branch since --into is not
// given), proving cmdMerge checks the branch the merge actually moves, not
// the source argument. The merge itself ("other", already main's own
// history) is a real, successful no-op ("Already up to date").
func TestCmdMergeReportsChangeSetDrift(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	base, ours, other, _ := buildResurrectionFixture(t, dir, "feat", "main")
	fabricateVersion(t, dir, "main", "merge", 9999999999, base, ours, other)
	stampVersionsFormat(t, dir)

	code, out, errb := runCLI(t, dir, "merge", other)
	if code != 0 {
		t.Fatalf("merge exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "main's change set") {
		t.Fatalf("stdout missing the change-set-changed statement for main:\n%s", out)
	}
	if !strings.Contains(out, "A f3.txt") {
		t.Fatalf("stdout missing the resurrected path f3.txt as Added:\n%s", out)
	}
}

// TestCmdPullNoDriftIsSilent proves `gg pull`'s wiring is harmless on the
// ordinary fast-forward path (no fabricated version, nothing to drift):
// same fixture as the existing TestPullFastForward.
func TestCmdPullNoDriftIsSilent(t *testing.T) {
	t.Parallel()
	clone := cloneBehind(t)
	code, out, errb := runCLI(t, clone, "pull")
	if code != 0 {
		t.Fatalf("pull exit %d: %s", code, errb)
	}
	if strings.Contains(out, "change set") {
		t.Fatalf("expected no drift summary for a fast-forward pull, got:\n%s", out)
	}
}

// TestCmdVersionsShowPrintsFrozenChangeSet covers the `gg versions show`
// extension: a version WITH endpoints prints its frozen change set (Base to
// Ours — the branch's OWN recorded contribution, not compared against
// anything current) via VersionPreview + CompareFiles.
func TestCmdVersionsShowPrintsFrozenChangeSet(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	base, ours, other, _ := buildResurrectionFixture(t, dir, "feat", "feat")
	fabricateVersion(t, dir, "feat", "rebase", 1700000000, base, ours, other)
	stampVersionsFormat(t, dir)

	code, out, errb := runCLI(t, dir, "versions", "show", "feat", "1700000000-rebase")
	if code != 0 {
		t.Fatalf("versions show exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "f3.txt") || !strings.Contains(out, "f4.txt") {
		t.Fatalf("versions show output missing the frozen change set:\n%s", out)
	}

	// "latest" resolves the same way `gg versions restore` does.
	code, out, errb = runCLI(t, dir, "versions", "show", "feat", "latest")
	if code != 0 {
		t.Fatalf("versions show latest exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "f3.txt") {
		t.Fatalf("versions show latest output missing f3.txt:\n%s", out)
	}
}

// TestCmdVersionsShowFieldlessRecordSaysNoPreview covers a one-branch op's
// record (amend, reset, undo-commit, delete-branch, restore): it has no
// endpoints by design, so `gg versions show` must state that plainly (exit
// 0) rather than surfacing VersionPreview's ErrNoPreview as an error.
func TestCmdVersionsShowFieldlessRecordSaysNoPreview(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	tip := runGit(t, dir, "rev-parse", "HEAD")
	body := "gg version snapshot (amend)\n\nGg-Meta: amend\n"
	syn := runGit(t, dir, "commit-tree", tip+"^{tree}", "-p", tip, "-m", body)
	ref := "refs/gg/versions/main/1700000100-amend"
	runGit(t, dir, "update-ref", ref, syn)
	stampVersionsFormat(t, dir)

	code, out, errb := runCLI(t, dir, "versions", "show", "main", "1700000100-amend")
	if code != 0 {
		t.Fatalf("versions show (fieldless) exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "no preview") {
		t.Fatalf("versions show (fieldless) output = %q, want a no-preview statement", out)
	}
}
