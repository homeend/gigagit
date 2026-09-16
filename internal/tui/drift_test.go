package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/changeset"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

// --- fixture helpers (the branch_push_realgit_test.go pushRealGit pattern) ---

func driftRunGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func driftWriteFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// driftInitRepo builds a real repo: branch "main", one commit, f.txt = "hi\n".
func driftInitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	driftRunGit(t, dir, "init", "-q", "-b", "main")
	driftRunGit(t, dir, "config", "user.email", "a@b.c")
	driftRunGit(t, dir, "config", "user.name", "a")
	driftWriteFile(t, dir, "f.txt", "hi\n")
	driftRunGit(t, dir, "add", "-A")
	driftRunGit(t, dir, "commit", "-qm", "init")
	return dir
}

// driftSvc builds a domain.Service over a real repo at dir.
func driftSvc(dir string) *domain.Service {
	return domain.New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))})
}

// driftStampVersionsFormat marks the store as format 2 (the versions
// feature's preflight-satisfied marker) — the internal/domain test helper of
// the same name, reproduced here since it is package-private there. Without
// it BranchVersions/DriftAfter read the store as an unmigrated format-1
// repo and refuse.
func driftStampVersionsFormat(t *testing.T, dir string) {
	t.Helper()
	driftRunGit(t, dir, "update-ref", git.MetaRef(domain.StoreVersions, domain.VersionsFormat), gitEmptyTreeSHA)
}

// gitEmptyTreeSHA is git's well-known empty-tree object, valid in every repo
// without being written first — the same marker value stampVersionsFormat
// (internal/domain) points the format ref at.
const gitEmptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// --- driftArmFor: pure, no I/O ---

// TestDriftArmForResolvesBranchPerOpType pins startOp's arming decision
// (op.go delegates to driftArmFor) for every op family the spec names
// (rebase/merge/pull) plus the TUI's own resume lane (ContinueOp), including
// the "" defaults-to-current-branch rung and the cherry-pick/revert
// no-arm case (those never record a two-branch version).
func TestDriftArmForResolvesBranchPerOpType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		op         engine.Operation
		current    string
		conflict   domain.ConflictState
		wantBranch string
		wantPaused bool
	}{
		{"rebase explicit branch", engine.SmartRebase{Branch: "feat", Onto: "main"}, "other", domain.ConflictState{}, "feat", false},
		{"rebase defaults to current", engine.SmartRebase{Onto: "main"}, "feat", domain.ConflictState{}, "feat", false},
		{"merge explicit target", engine.SmartMerge{Source: "feat", Target: "main"}, "other", domain.ConflictState{}, "main", false},
		{"merge defaults to current", engine.SmartMerge{Source: "feat"}, "main", domain.ConflictState{}, "main", false},
		{"pull explicit branch", engine.SmartPull{Branch: "feat"}, "other", domain.ConflictState{}, "feat", false},
		{"pull defaults to current", engine.SmartPull{}, "main", domain.ConflictState{}, "main", false},
		{"interactive rebase always explicit", engine.InteractiveRebase{Branch: "feat", Onto: "main"}, "other", domain.ConflictState{}, "feat", false},
		{"continue resumes a paused rebase: Source", engine.ContinueOp{}, "irrelevant", domain.ConflictState{Op: "rebase", Source: "feat", Target: "main"}, "feat", true},
		{"continue resumes a paused merge: Target", engine.ContinueOp{}, "irrelevant", domain.ConflictState{Op: "merge", Source: "feat", Target: "main"}, "main", true},
		{"continue resumes a cherry-pick: not armed", engine.ContinueOp{}, "irrelevant", domain.ConflictState{Op: "cherry-pick", Source: "abc123"}, "", false},
		{"continue resumes a revert: not armed", engine.ContinueOp{}, "irrelevant", domain.ConflictState{Op: "revert", Source: "abc123"}, "", false},
		{"an unrelated op arms nothing", engine.Fetch{}, "main", domain.ConflictState{}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			branch, paused := driftArmFor(tc.op, tc.current, tc.conflict)
			if branch != tc.wantBranch || paused != tc.wantPaused {
				t.Fatalf("driftArmFor(%#v, %q, %+v) = (%q, %v), want (%q, %v)",
					tc.op, tc.current, tc.conflict, branch, paused, tc.wantBranch, tc.wantPaused)
			}
		})
	}
}

// --- versions popup onEnter: frozen preview vs. today's commit view ---

// TestVersionsPopupEnterWithEndpointsOpensFrozenPreview: a version row with
// recorded endpoints opens the compare layer anchored at the FROZEN
// Base/Ours — never a live branch tip. m.branches deliberately carries a
// DIFFERENT hash for the same branch name, so a test that accidentally read
// the live tip (the pre-Task-11 behavior) would fail here.
func TestVersionsPopupEnterWithEndpointsOpensFrozenPreview(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40}
	m.branches = []model.Branch{{Name: "main", Hash: "livetiphash000000"}} // NOT Base/Ours
	p := &versionsPopup{
		mode:   versionsModeVersions,
		branch: "main",
		rows: []model.BranchVersion{
			{
				Ref: "refs/gg/versions/main/1753100000-rebase", Hash: "snaphash0000000000",
				Subject: "did a rebase", Op: "rebase", Unix: 1753100000,
				Base: "1111111111111111", Ours: "2222222222222222",
			},
		},
	}
	m = m.pushLayer(p)

	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)

	if m.filesView == nil || !m.inCompareMode() {
		t.Fatal("a version row with recorded endpoints should open the frozen preview in compare mode")
	}
	if m.filesLeft.Hash() != "1111111111111111" || m.filesRight.Hash() != "2222222222222222" {
		t.Fatalf("endpoints = %+v/%+v, want the recorded Base/Ours — never the live tip (livetiphash000000)", m.filesLeft, m.filesRight)
	}
}

// TestVersionsPopupEnterFieldlessOpensCommitView: a fieldless record (a
// one-branch op — amend/reset/undo-commit/delete-branch/restore) has no
// Base/Ours, exactly what domain.VersionPreview reports as ErrNoPreview.
// That must never surface as a user-facing error — it opens today's commit
// view instead, exactly like enter on an ordinary commit row.
func TestVersionsPopupEnterFieldlessOpensCommitView(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40}
	p := &versionsPopup{
		mode:   versionsModeVersions,
		branch: "main",
		rows: []model.BranchVersion{
			{Ref: "refs/gg/versions/main/1753100000-amend", Hash: "amendhash00000000", Subject: "amended message", Op: "amend", Unix: 1753100000},
		},
	}
	m = m.pushLayer(p)

	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)

	if m.filesView == nil || m.inCompareMode() {
		t.Fatal("a fieldless record should open today's commit view, never the compare layer")
	}
	if m.filesHash != "amendhash00000000" {
		t.Fatalf("filesHash = %q, want the version's own commit hash", m.filesHash)
	}
	if m.statusMsg != "" {
		t.Fatalf("statusMsg = %q, ErrNoPreview must never surface as a user-facing error", m.statusMsg)
	}
	if layerOf[*versionsPopup](m) != nil {
		t.Fatal("the versions popup should close once the commit view opens")
	}
}

// --- the notice itself: drift and/or paused-for-conflicts ---

// TestDriftCheckCmdReportsResurrectionNamingPath is the end-to-end
// motivating case (mirrors internal/domain's
// TestDriftSinceReportsResurrectionAfterRebase): main has f3.txt; branch
// feat modifies it and adds f4.txt; main then deletes f3.txt. Rebasing feat
// onto main hits a modify/delete conflict on f3.txt, resolved by KEEPING the
// file. feat now reads as ADDING f3.txt — a file it never added — which
// driftCheckCmd (the same DriftAfter call opFinishedMsg dispatches) must
// resolve on its own from the branch name alone, and the resulting notice
// must name the resurrected path.
func TestDriftCheckCmdReportsResurrectionNamingPath(t *testing.T) {
	t.Parallel()
	dir := driftInitRepo(t) // main, f.txt = "hi\n"
	svc := driftSvc(dir)
	ctx := context.Background()

	driftWriteFile(t, dir, "f3.txt", "hi\n")
	driftRunGit(t, dir, "add", "-A")
	driftRunGit(t, dir, "commit", "-qm", "main: add f3.txt")
	baseSha := driftRunGit(t, dir, "rev-parse", "main")

	driftRunGit(t, dir, "checkout", "-q", "-b", "feat")
	driftWriteFile(t, dir, "f3.txt", "changed\n")
	driftWriteFile(t, dir, "f4.txt", "hi\n")
	driftRunGit(t, dir, "add", "-A")
	driftRunGit(t, dir, "commit", "-qm", "feat: modify f3, add f4")
	oursSha := driftRunGit(t, dir, "rev-parse", "feat")

	driftRunGit(t, dir, "checkout", "-q", "main")
	driftRunGit(t, dir, "rm", "-q", "f3.txt")
	driftRunGit(t, dir, "commit", "-qm", "main: delete f3")
	otherSha := driftRunGit(t, dir, "rev-parse", "main")

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
	driftStampVersionsFormat(t, dir)

	// Rebase feat onto main; resolve the modify/delete conflict by KEEPING
	// f3.txt (git add, not git rm) — the resurrection.
	driftRunGit(t, dir, "checkout", "-q", "feat")
	rebaseCmd := exec.Command("git", "rebase", "main")
	rebaseCmd.Dir = dir
	_ = rebaseCmd.Run() // tolerated: the expected modify/delete conflict exits non-zero
	driftRunGit(t, dir, "add", "f3.txt")
	// -c core.editor=true pins the editor: --continue after a modify/delete
	// resolution launches $GIT_EDITOR to confirm the message.
	driftRunGit(t, dir, "-c", "core.editor=true", "rebase", "--continue")

	m := Model{svc: svc}
	msg := m.driftCheckCmd("feat", false, 0)().(driftCheckMsg)
	if msg.err != nil {
		t.Fatalf("driftCheckCmd err = %v", msg.err)
	}
	if !msg.report.Checked || !msg.report.Report.Drifted() {
		t.Fatalf("report = %+v, want Checked && Drifted", msg.report)
	}

	mm, _ := m.Update(msg)
	m = mm.(Model)

	if len(m.notices) != 1 {
		t.Fatalf("notices = %+v, want exactly one", m.notices)
	}
	n := m.notices[0]
	if !strings.Contains(n.title, "feat") {
		t.Fatalf("notice title = %q, want it to name the branch", n.title)
	}
	found := false
	for _, line := range n.detail {
		if strings.Contains(line, "f3.txt") {
			found = true
		}
	}
	if !found {
		t.Fatalf("notice detail = %v, want a line naming the resurrected path f3.txt", n.detail)
	}
	if !m.noticesUnread {
		t.Fatal("a freshly raised notice must be unread (drives the blink)")
	}
}

// TestDriftNoticePausedWithoutDriftStillRaised covers the spec's OTHER
// trigger: an operation that paused for conflicts and then completed via
// resume, even when the resulting change set does not itself read as
// drifted (report.Checked but not Drifted()). "Completed via resume" is
// itself the signal — no flag is persisted, and this must not require a
// real git repo to prove.
func TestDriftNoticePausedWithoutDriftStillRaised(t *testing.T) {
	t.Parallel()
	m := Model{}
	report := domain.DriftReport{Ref: "refs/gg/versions/feat/1700000000-rebase", Checked: true} // no Added/Removed: not drifted
	m, cmd := m.applyDriftReport("feat", report, true /* paused */)

	if len(m.notices) != 1 {
		t.Fatalf("notices = %+v, want exactly one (paused alone is grounds to raise)", m.notices)
	}
	n := m.notices[0]
	if !strings.Contains(n.title, "feat") {
		t.Fatalf("notice title = %q, want it to name the branch", n.title)
	}
	// report.Checked == true: a comparison genuinely ran and found no drift,
	// so it is accurate to say the change set still matches.
	if !containsAny(n.detail, "still matches the recorded version") {
		t.Fatalf("detail = %v, want the Checked wording (a comparison really ran)", n.detail)
	}
	if containsAny(n.detail, "Nothing was recorded to compare") {
		t.Fatalf("detail = %v, must not claim nothing was recorded when Checked is true", n.detail)
	}
	if cmd == nil {
		t.Fatal("a newly raised notice must arm the blink tick")
	}
}

// TestDriftNoticePausedUncheckedUsesUncheckedWording covers the fix-round
// finding: report.Checked == false means DriftAfter never actually compared
// anything (nothing recorded, or the feature is off) — the paused-only
// detail text must not then claim "its change set still matches the
// recorded version", since no comparison ran to support that claim.
func TestDriftNoticePausedUncheckedUsesUncheckedWording(t *testing.T) {
	t.Parallel()
	m := Model{}
	report := domain.DriftReport{Checked: false} // nothing recorded to compare against
	m, cmd := m.applyDriftReport("feat", report, true /* paused */)

	if len(m.notices) != 1 {
		t.Fatalf("notices = %+v, want exactly one (paused alone is grounds to raise, even unchecked)", m.notices)
	}
	n := m.notices[0]
	if !containsAny(n.detail, "Nothing was recorded to compare") {
		t.Fatalf("detail = %v, want the Unchecked wording", n.detail)
	}
	if containsAny(n.detail, "still matches the recorded version") {
		t.Fatalf("detail = %v, must not claim a match when Checked is false — no comparison ran", n.detail)
	}
	if cmd == nil {
		t.Fatal("a newly raised notice must arm the blink tick")
	}
}

// containsAny reports whether any line in lines contains substr.
func containsAny(lines []string, substr string) bool {
	for _, l := range lines {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

// TestDriftNoticeDedupesByVersionRef is the fix-round regression guard: two
// applyDriftReport calls sharing the same branch+Ref (a paused rebase raises
// on ref V, then a later fast-forward pull on the same branch arms, records
// no new version, and DriftAfter re-reports V) must produce exactly ONE
// notice/source, not two sharing an id — which removeNotice (matching by id)
// would then drop together on a single Dismiss.
func TestDriftNoticeDedupesByVersionRef(t *testing.T) {
	t.Parallel()
	m := Model{}
	ref := "refs/gg/versions/feat/1700000000-rebase"

	// First: paused, not drifted.
	m, _ = m.applyDriftReport("feat", domain.DriftReport{Ref: ref, Checked: true}, true)
	if len(m.notices) != 1 || len(m.driftNotices) != 1 {
		t.Fatalf("after first report: notices=%d driftNotices=%d, want 1/1", len(m.notices), len(m.driftNotices))
	}
	firstID := m.notices[0].id

	// Second: same ref, this time genuinely drifted (a later re-check of the
	// SAME version could plausibly report differently) — must REPLACE, not add.
	report2 := domain.DriftReport{
		Ref: ref, Checked: true,
		Report: changeset.Report{Added: []changeset.Entry{{Status: 'A', Path: "resurrected.txt"}}},
	}
	m, _ = m.applyDriftReport("feat", report2, false)

	if len(m.notices) != 1 {
		t.Fatalf("notices = %+v, want exactly one (same ref must replace, not duplicate)", m.notices)
	}
	if len(m.driftNotices) != 1 {
		t.Fatalf("driftNotices = %+v, want exactly one", m.driftNotices)
	}
	if m.notices[0].id != firstID {
		t.Fatalf("id = %q, want the same stable id %q (keyed on the version ref)", m.notices[0].id, firstID)
	}
	if !containsAny(m.notices[0].detail, "resurrected.txt") {
		t.Fatalf("detail = %v, want the SECOND (replacing) report's content", m.notices[0].detail)
	}

	// A single Dismiss (removeNotice) must clear both the rendered notice
	// and its source — not leave a hidden duplicate source that resurrects
	// the notice on the next rebuildNotices (a health re-read, a language
	// switch).
	m = m.removeNotice(firstID)
	if len(m.notices) != 0 || len(m.driftNotices) != 0 {
		t.Fatalf("after removeNotice: notices=%d driftNotices=%d, want 0/0", len(m.notices), len(m.driftNotices))
	}
}

// TestDriftNoticeNoDriftNoPauseIsSilent: neither trigger fires (a clean
// rebase/merge/pull that never paused) — no notice, matching the CLI's own
// silent-path contract (Task 10's TestCmdRebaseNoDriftIsSilent).
func TestDriftNoticeNoDriftNoPauseIsSilent(t *testing.T) {
	t.Parallel()
	m := Model{}
	report := domain.DriftReport{Ref: "refs/gg/versions/feat/1700000000-rebase", Checked: true}
	m, cmd := m.applyDriftReport("feat", report, false)

	if len(m.notices) != 0 {
		t.Fatalf("notices = %+v, want none", m.notices)
	}
	if cmd != nil {
		t.Fatal("nothing to say: no blink should be armed")
	}
}

// --- opFinishedMsg wiring: only dispatched on a real, unpaused-error success ---

// TestOpFinishedMsgDispatchesDriftCheckOnChangedSuccess drives the actual
// opFinishedMsg handler (not driftCheckCmd/applyDriftReport directly) with
// m.pendingDriftBranch/Paused pre-armed — as startOp would have left them —
// and confirms the batched command it returns really does carry a
// driftCheckCmd for that branch, and that the pending fields are cleared
// unconditionally either way.
func TestOpFinishedMsgDispatchesDriftCheckOnChangedSuccess(t *testing.T) {
	t.Parallel()
	dir := driftInitRepo(t)
	svc := driftSvc(dir)

	m := Model{svc: svc, status: model.WorkingTreeStatus{Branch: "main"}}
	m.pendingDriftBranch = "main"
	m.pendingDriftPaused = true
	m.pendingSources = []sourceKey{srcStatus} // keep the batch small: one real reload + driftCmd

	mm, cmd := m.Update(opFinishedMsg{res: engine.Result{Changed: true}, err: nil})
	m = mm.(Model)

	if m.pendingDriftBranch != "" || m.pendingDriftPaused {
		t.Fatalf("pending drift fields must be cleared unconditionally, got branch=%q paused=%v", m.pendingDriftBranch, m.pendingDriftPaused)
	}
	if cmd == nil {
		t.Fatal("expected a batched command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected a tea.BatchMsg, got %T", cmd())
	}
	var found bool
	for _, c := range batch {
		if c == nil {
			continue
		}
		if dm, ok := c().(driftCheckMsg); ok {
			found = true
			if dm.branch != "main" || !dm.paused {
				t.Fatalf("driftCheckMsg = %+v, want branch=main paused=true", dm)
			}
		}
	}
	if !found {
		t.Fatal("no driftCheckMsg found in the batched command")
	}
}

// TestOpFinishedMsgSkipsDriftCheckOnError confirms a failed op (the
// paused-and-NOT-resumed case a plain rebase/merge/pull returns — see
// internal/cli's Task 10 report) never dispatches a drift check: there is no
// "after" side to compare against an operation that never completed.
func TestOpFinishedMsgSkipsDriftCheckOnError(t *testing.T) {
	t.Parallel()
	dir := driftInitRepo(t)
	m := Model{svc: driftSvc(dir), status: model.WorkingTreeStatus{Branch: "main"}}
	m.pendingDriftBranch = "main"
	m.pendingDriftPaused = false
	m.pendingSources = []sourceKey{srcStatus}

	mm, cmd := m.Update(opFinishedMsg{res: engine.Result{}, err: context.DeadlineExceeded})
	m = mm.(Model)

	if m.pendingDriftBranch != "" {
		t.Fatalf("pendingDriftBranch = %q, want cleared even on error", m.pendingDriftBranch)
	}
	if cmd == nil {
		return // no batch at all: trivially no drift check
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			if _, ok := c().(driftCheckMsg); ok {
				t.Fatal("a failed op must never dispatch a drift check")
			}
		}
	}
}
