package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// --- O: the one refusal before the slots exist is said ---

func TestSwitchViewBeforeTheSlotsAreSeededSaysSo(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m.home, m.viewed = "", "" // a repo switch in flight: the list landed, the slots have not
	nm, ok := m.switchView(other)
	if ok || nm.statusMsg == "" {
		t.Fatalf("ok=%v status=%q; want a refusal with a reason", ok, nm.statusMsg)
	}
}

// --- P/Q: the steer leftovers wait in their worktree ---

// A parked agent tour is the leaving slot's: B's status must not show it,
// and it is back when A returns.
func TestSwitchViewParksAParkedTour(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m.tour = "ov-1"
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if m.tour != "" {
		t.Fatalf("tour %q crossed the swap: it would show over another worktree's status", m.tour)
	}
	m, _ = m.switchView(home)
	if m.tour != "ov-1" {
		t.Fatalf("tour = %q back in its worktree, want ov-1", m.tour)
	}
}

// Attention marks on a working file wait in their worktree; a commit's are
// the repository's and stay.
func TestSwitchViewParksWorkingTreeAttentionMarks(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	working := attentionKey{path: "a.txt", state: "unstaged"}
	committed := attentionKey{path: "a.txt", state: "commit", commit: "abc1234"}
	m.attention = map[attentionKey][]steerMark{
		working:   {{side: "new", start: 1, end: 2, tone: "warn"}},
		committed: {{side: "new", start: 1, end: 2, tone: "warn"}},
	}
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if _, kept := m.attention[working]; kept {
		t.Fatal("a working-file mark crossed the swap: it would band another worktree's file")
	}
	if _, kept := m.attention[committed]; !kept {
		t.Fatal("a commit's mark was dropped: commits are the repository's")
	}
	m, _ = m.switchView(home)
	if marks := m.attention[working]; len(marks) != 1 || marks[0].start != 1 {
		t.Fatalf("the working-file mark did not come back: %v", m.attention)
	}
}

// A navigate parked for ONE status re-read of home waits in home: the
// worktree whose status lands next does not run it, and it is still parked
// when home returns.
func TestParkedStatusRetryWaitsInItsWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	if err := os.WriteFile(filepath.Join(other, "only-there.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := steer.Command{Cmd: "navigate", File: "only-there.txt"}
	m, _ = m.steerNavigateStatusFile(c, false)
	if m.pendingSteer == nil || m.pendingSteer.stage != steerStageStatusRetry {
		t.Fatalf("precondition: parked for a status retry, got %+v", m.pendingSteer)
	}
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if m.pendingSteer != nil {
		t.Fatalf("A's parked navigate crossed the swap: %+v", m.pendingSteer)
	}
	m = landView(t, m)
	if m.topLayer() != nil {
		t.Fatalf("the retry opened %T over the OTHER worktree's file", m.topLayer())
	}
	m, _ = m.switchView(home)
	if m.pendingSteer == nil || m.pendingSteer.stage != steerStageStatusRetry {
		t.Fatalf("the navigate is not parked back in its worktree: %+v", m.pendingSteer)
	}
}

// --- the hosted page is rooted at home: every start/reroot passes homeSvc ---

func TestWebHostAlwaysRootsAtHome(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("webhost.go")
	if err != nil {
		t.Fatal(err)
	}
	calls := regexp.MustCompile(`(startWebCmd|rerootWebCmd)\(([^\n]*)`).FindAllStringSubmatch(string(src), -1) // the call's line
	if len(calls) < 3 {
		t.Fatalf("found %d web host start/reroot calls, expected several", len(calls))
	}
	for _, c := range calls {
		if strings.Contains(c[2], "*domain.Service") || strings.Contains(c[2], "WebHost") {
			continue // the definitions
		}
		if !strings.Contains(c[2], "m.homeSvc()") {
			t.Fatalf("%s is rooted at %q, want m.homeSvc() (the page follows gg's own worktree, never a look)", c[1], strings.TrimSpace(c[2]))
		}
	}
}

var _ = time.Now
var _ = model.KeyOf
