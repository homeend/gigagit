package tui

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/steer"
)

// A navigate parked in a sleeping member's slot expires like one parked
// in the live member's: its sender is answered, never left to time out.
func TestParkedSteerInSleepingMemberExpires(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	a := m.homeRepo()
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	nm, ok = nm.switchRepo(a, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	v := b.views[b.key]
	if v == nil {
		t.Fatal("no B slot")
	}
	v.windows.pendingSteer = &pendingSteer{cmd: steer.Command{ID: "p1", Cmd: "navigate", File: "x.go"}, at: time.Now().Add(-time.Hour)}
	m, _ = m.expireParkedSteer(time.Now()) // (no inbox here: the answer itself is a nil cmd)
	if v.windows.pendingSteer != nil {
		t.Fatal("a navigate parked in member B's slot did not expire")
	}
	// Dropping a member answers what its slots still owe.
	v.windows.pendingSteer = &pendingSteer{cmd: steer.Command{ID: "p2", Cmd: "navigate", File: "y.go"}, at: time.Now()}
	_, _ = m.dropMember(b)
	if v.windows.pendingSteer != nil {
		t.Fatal("dropping the member left its parked navigate unanswered")
	}
}

// A commit-message result for a sleeping member's worktree is kept as
// THAT worktree's pending message (c there opens the box filled).
func TestCommitMessageForSleepingMemberIsPending(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	m, _ = m.applyCommitMessage(domain.TaskInfo{Worktree: b.mainPath, Result: "subject\n\nbody", Agent: "claude", Key: "commit_message"})
	if _, ok := m.pendingCommitMsg[pendingKey(b.mainPath)]; !ok {
		t.Fatalf("B's generated message was not kept as its pending message: %q", m.statusMsg)
	}
}

// An agent in member B revealing one of B's commits while A is shown is
// asked about as a switch (the commit is B's), never refused as "not
// loaded" from A's feed.
func TestSteerCommitFromOtherMemberAsksToSwitch(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	gittest.Run(t, b.mainPath, "commit", "-q", "--allow-empty", "-m", "b-only")
	m = settleWorkspace(t, m, memberLoadCmd(b))
	out, err := exec.Command("git", "-C", b.mainPath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.TrimSpace(string(out))
	nm, _ := m.applySteer(steer.Command{ID: "t1", Cmd: "navigate", Commit: hash, Worktree: b.mainPath})
	m = nm
	if m.liveRepo() != b && m.steerAsk == nil {
		t.Fatalf("B's commit was neither shown nor asked about: status=%q", m.statusMsg)
	}
}

// The drop prompt's answer marks the members asked; a follow-up that is
// refused (the switch away is held by an operation) unmarks them, so the
// next attempt asks again.
func TestDropPromptAsksAgainAfterARefusedFollowUp(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	_ = startTestSession(t, m, "sleep 5")
	if _, err := domain.Sessions().Start(sessionSpecForTest("in-b", b.mainPath)); err != nil {
		t.Fatal(err)
	}
	nm, _, asked := m.askBeforeDrop([]*repoView{b}, func(m Model) (Model, tea.Cmd) {
		m.statusMsg = "refused"
		return m, nil // nothing dropped
	})
	if !asked {
		t.Fatal("no prompt")
	}
	m = nm
	m, _ = wsPress(t, m, "enter") // leave running → the follow-up runs and refuses
	if b.dropAnswered {
		t.Fatal("a refused follow-up left the member marked as answered")
	}
	_, _, asked = m.askBeforeDrop([]*repoView{b}, func(m Model) (Model, tea.Cmd) { return m, nil })
	if !asked {
		t.Fatal("the next drop did not ask")
	}
}
