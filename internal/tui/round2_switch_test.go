package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// --- B: a console shown while the panels cannot swap queues the swap ---

// alt+a on B's console while an op runs in home: the console shows, the
// panels stay on the op's worktree, and swap to B's once the op ends.
func TestShowConsoleDuringAnOpSwapsWhenItEnds(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m.running = true
	m, _ = m.showConsole(id, false)
	if m.console == nil || m.viewed != m.home || m.pendingReturnView != model.KeyOf(other) {
		t.Fatalf("console=%v viewed=%q pending=%q; want the console shown, the swap queued", m.console != nil, m.viewed, m.pendingReturnView)
	}
	nm, _ := m.Update(opFinishedMsg{})
	m = nm.(Model)
	if m.viewed != model.KeyOf(other) || m.pendingReturnView != "" {
		t.Fatalf("after the op: viewed=%q pending=%q", m.viewed, m.pendingReturnView)
	}
}

// The console closed before the queued swap happened: the panels stay
// where they are — the return point is already on screen.
func TestCloseConsoleBeforeAQueuedShowCancelsIt(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m.running = true
	m, _ = m.showConsole(id, false)
	m = m.closeConsole()
	if m.pendingReturnView != "" {
		t.Fatalf("pending=%q after the close; want nothing queued", m.pendingReturnView)
	}
	nm, _ := m.Update(opFinishedMsg{})
	m = nm.(Model)
	if m.viewed != m.home {
		t.Fatalf("viewed=%q after the op; want home", m.viewed)
	}
}

// --- C: the commit feed walks from the viewed worktree ---

// A commit only the viewed worktree's detached HEAD reaches shows in
// Commits once the slot's kick lands.
func TestViewedDetachedHeadCommitsShow(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", other}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("checkout", "-q", "--detach")
	if err := os.WriteFile(filepath.Join(other, "loose.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "loose.txt")
	git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "loose")
	out, err := exec.Command("git", "-C", other, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	loose := strings.TrimSpace(string(out))
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	m = landView(t, m)
	for _, c := range m.commits {
		if c.Hash == loose {
			return
		}
	}
	t.Fatalf("the viewed worktree's detached commit %s is not in Commits (%d rows): the feed still walks home", loose, len(m.commits))
}

// --- D: the slot's reviews decide its rows ---

// Home has a current working review; slot B has none: B's Files panel
// has no Review row (the rows are derived from the ARRIVING slot's reviews).
func TestLoadViewDerivesRowsFromTheSlotsReviews(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m.workingReviews = []domain.WorkingReview{{WorkingReviewMatch: domain.WorkingReviewMatch{Current: true}}}
	m = m.withStatus(m.status)
	if m.filesIdxReview == nil {
		t.Fatal("precondition: home shows a Review row")
	}
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if m.filesIdxReview != nil || len(m.workingReviews) != 0 {
		t.Fatalf("phantom Review row over B: idx=%v reviews=%d", m.filesIdxReview, len(m.workingReviews))
	}
}

// --- E: a files view waits in its worktree, whatever it lists ---

// A commit's files view (enter on a commit) waits in the worktree it was
// opened in like any other window: absent over B, back in A.
func TestSwitchViewParksACommitsFilesWindow(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m.filesView = &contentPopup{}
	m.filesMode = filesModeChanged
	m.filesHash = "abc1234"
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if m.filesView != nil || m.filesHash != "" {
		t.Fatal("a commit's files view crossed the swap")
	}
	m, _ = m.switchView(home)
	if m.filesView == nil || m.filesHash != "abc1234" {
		t.Fatal("the commit's files view did not come back")
	}
}

// --- F: a refused steer switch to home arms no replay ---

// The agent asks for gg's own worktree while another is viewed and an op
// runs: the swap is refused and NOTHING is replayed against the viewed slot.
func TestSteerAskForHomeRefusedArmsNoReplay(t *testing.T) {
	m := loadedModel(t)
	m, other := viewedOther(t, m)
	m.running = true
	c := steer.Command{Cmd: "navigate", File: "hi.txt", Worktree: m.homeWorktree()}
	n := steerAskNotice(&steerSwitchAsk{cmd: c, from: "x"}, "")
	m, _ = n.actions[0].run(m)
	if m.viewed != model.KeyOf(other) {
		t.Fatalf("viewed=%q; want the swap refused", m.viewed)
	}
	if m.startAtCmd != nil || m.startAtPending {
		t.Fatalf("a replay is armed against the viewed slot: %+v", m.startAtCmd)
	}
}

// --- G: the resume prompt's one-shot flag is the slot's ---

// The prompt fired for B's paused op; a round trip through home (whose
// status re-arms ITS flag) must not fire it again on B.
func TestResumePromptFlagIsPerSlot(t *testing.T) {
	m := loadedModel(t)
	m, other := viewedOther(t, m)
	m.conflict = domain.ConflictState{Op: "merge"}
	m.resumePromptShown = true
	m, _ = m.switchView(m.homeWorktree())
	m = m.maybeResumePrompt() // home's status: not paused
	m, _ = m.switchView(other)
	if !m.resumePromptShown {
		t.Fatal("B's one-shot flag was lost on the round trip: the prompt would fire again")
	}
}

// A compare files view waits in its worktree — a working-tree side or not.
func TestSwitchViewParksACompareWithAWorkingSide(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m.filesView = &contentPopup{}
	m.filesMode = filesModeCompare
	m.filesLeft, m.filesRight = commitEP(t, "abc1234"), model.WorkTreeEndpoint()
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if m.filesView != nil {
		t.Fatal("a compare against the working tree crossed the swap")
	}
	m, _ = m.switchView(home)
	if m.filesView == nil || m.filesRight != model.WorkTreeEndpoint() {
		t.Fatal("the compare did not come back with its sides")
	}
}

func commitEP(t *testing.T, hash string) model.Endpoint {
	t.Helper()
	ep, err := model.CommitEndpoint(hash)
	if err != nil {
		t.Fatal(err)
	}
	return ep
}
