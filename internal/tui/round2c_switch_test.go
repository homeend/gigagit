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

// --- P: a parked agent tour is the leaving slot's ---

func TestSwitchViewDropsAParkedTour(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m.tour = "ov-1"
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if m.tour != "" {
		t.Fatalf("tour %q survived the swap: it would show over another worktree's status", m.tour)
	}
}

// --- Q: steer leftovers do not cross slots ---

// Attention marks on a working file are the leaving tree's; a commit's stay.
func TestSwitchViewDropsWorkingTreeAttentionMarks(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m.attention = map[attentionKey][]steerMark{
		{path: "a.txt", state: "unstaged"}:                  {{side: "new", start: 1, end: 2, tone: "warn"}},
		{path: "a.txt", state: "commit", commit: "abc1234"}: {{side: "new", start: 1, end: 2, tone: "warn"}},
	}
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if _, kept := m.attention[attentionKey{path: "a.txt", state: "unstaged"}]; kept {
		t.Fatal("a working-file mark survived the swap: it would band another worktree's file")
	}
	if _, kept := m.attention[attentionKey{path: "a.txt", state: "commit", commit: "abc1234"}]; !kept {
		t.Fatal("a commit's mark was dropped: commits are the repository's")
	}
}

// A navigate parked for ONE status re-read of home must not run against
// the worktree whose status lands next: it is answered as failed instead.
func TestParkedStatusRetryDoesNotRunAgainstAnotherWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
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
	m = landView(t, m)
	if m.pendingSteer != nil {
		t.Fatalf("still parked after another worktree's status landed: %+v", m.pendingSteer)
	}
	if m.topLayer() != nil {
		t.Fatalf("the retry opened %T over the OTHER worktree's file", m.topLayer())
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
