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

// --- E: only the F window (files on disk) is the leaving tree's ---

// A commit's files view (enter on a commit) is the repository's and
// survives the swap; the F window of files on disk does not.
func TestSwitchViewKeepsACommitsFilesWindow(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m.filesView = &contentPopup{}
	m.filesMode = filesModeChanged
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if m.filesView == nil {
		t.Fatal("a commit's files view was closed by the swap")
	}
	m.filesView = &contentPopup{}
	m.filesMode = filesModeWorktree
	m, _ = m.switchView(m.homeWorktree())
	if m.filesView != nil {
		t.Fatal("the F window (files on disk) survived the swap")
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
