package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// recycleModel: Branches tab; "loose" is checked out nowhere, "feature" is
// in a linked worktree, "main" is the current worktree's branch.
func recycleModel() Model {
	m := showInWorktreesModel()
	m.currentWorktree = "/repo"
	m.sel[panelBranches] = 2 // "loose"
	return m
}

func TestRecycleRowOfferedOnlyForUncheckedOutBranch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		sel  int
		want bool
	}{
		{0, false}, // feature: in a worktree → Show in Worktrees instead
		{1, false}, // main: current worktree
		{2, true},  // loose
	} {
		m := recycleModel()
		m.sel[panelBranches] = tc.sel
		got := ids(availableActions(m))
		if got["recycle-worktree"] != tc.want {
			t.Errorf("branch %q: recycle-worktree offered = %v, want %v", m.branches[tc.sel].Name, got["recycle-worktree"], tc.want)
		}
		if got["recycle-worktree"] && got["show-in-worktrees"] {
			t.Errorf("branch %q: the two rows must be mutually exclusive", m.branches[tc.sel].Name)
		}
	}
}

func TestRecycleRowHiddenWhenNoOtherWorktree(t *testing.T) {
	t.Parallel()
	m := recycleModel()
	m.worktrees = []model.Worktree{{Path: "/repo", Branch: "main"}}
	if ids(availableActions(m))["recycle-worktree"] {
		t.Fatal("no candidate worktree → no row")
	}
}

func TestRecyclePickerListsOtherWorktreesOnly(t *testing.T) {
	t.Parallel()
	m := recycleModel()
	m.worktrees = append(m.worktrees, model.Worktree{Path: "/repo-bare", Bare: true}, model.Worktree{Path: "/repo-wt/det", Detached: true})
	row, ok := rowByID(availableActions(m), "recycle-worktree")
	if !ok {
		t.Fatal("row not offered")
	}
	nm, _ := row.run(m)
	m = nm.(Model)
	if m.actionMenu == nil {
		t.Fatal("picker must re-populate the action menu")
	}
	got := ids(m.actionMenu.rows)
	for _, want := range []string{"recycle-into:/repo-wt/other", "recycle-into:/repo-wt/feature", "recycle-into:/repo-wt/det"} {
		if !got[want] {
			t.Errorf("missing picker row %s (have %v)", want, got)
		}
	}
	for _, no := range []string{"recycle-into:/repo", "recycle-into:/repo-bare"} {
		if got[no] {
			t.Errorf("picker must not list %s", no)
		}
	}
	// Labels: path elided in the middle + the current branch / "detached".
	var sawDetached bool
	for _, r := range m.actionMenu.rows {
		if strings.HasSuffix(r.id, "/det") && strings.Contains(r.label, "detached") {
			sawDetached = true
		}
	}
	if !sawDetached {
		t.Fatal("detached worktree row must say detached")
	}
}

// A long worktree path is elided in the MIDDLE to fit the menu's text width
// with the branch name still visible at the end of the row (the menu box is
// narrower than the screen, so the budget is the menu's, not the screen's).
func TestRecyclePickerRowFitsTheMenuWidth(t *testing.T) {
	t.Parallel()
	m := recycleModel()
	m.width, m.height = 120, 40
	long := "/tmp/claude-1000/-mnt-t-others-gigagit/22501cfc-34d0-48fe-8b7d-ff6528344fbf/scratchpad/rc/wt"
	m.worktrees = append(m.worktrees, model.Worktree{Path: long, Branch: "wt-branch"})
	row, _ := rowByID(availableActions(m), "recycle-worktree")
	nm, _ := row.run(m)
	m = nm.(Model)
	pick, ok := rowByID(m.actionMenu.rows, "recycle-into:"+long)
	if !ok {
		t.Fatal("picker row missing")
	}
	w, _ := m.overlayDims()
	textW := popupTextWidth(popupInnerWidth(w))
	if got := len([]rune(pick.label)) + 2; got > textW { // "> " prefix
		t.Fatalf("row is %d cols, menu text width is %d: %q", got, textW, pick.label)
	}
	if !strings.HasSuffix(pick.label, "  wt-branch") || !strings.HasSuffix(strings.TrimSuffix(pick.label, "  wt-branch"), "/wt") {
		t.Fatalf("label must keep the path tail and the branch: %q", pick.label)
	}
}

func TestRecyclePickerEnterStartsTheOp(t *testing.T) {
	t.Parallel()
	m := recycleModel()
	row, _ := rowByID(availableActions(m), "recycle-worktree")
	nm, _ := row.run(m)
	m = nm.(Model)
	pick, ok := rowByID(m.actionMenu.rows, "recycle-into:/repo-wt/other")
	if !ok {
		t.Fatal("picker row missing")
	}
	nm, cmd := pick.run(m)
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("expected the op to start")
	}
	if !m.running || m.opName != engine.OpName(engine.RecycleWorktree{}) {
		t.Fatalf("running=%v opName=%q, want a running RecycleWorktree", m.running, m.opName)
	}
}

// Review Focus 4: the picker acts on the branch captured when it opened.
func TestRecyclePickerUsesCapturedBranch(t *testing.T) {
	t.Parallel()
	m := recycleModel()
	row, _ := rowByID(availableActions(m), "recycle-worktree")
	nm, _ := row.run(m)
	m = nm.(Model)
	m.sel[panelBranches] = 0 // a background refresh moved the cursor
	if m.recycleBranch != "loose" {
		t.Fatalf("recycleBranch = %q, want loose (captured at open)", m.recycleBranch)
	}
	pick, _ := rowByID(m.actionMenu.rows, "recycle-into:/repo-wt/other")
	nm, _ = pick.run(m)
	m = nm.(Model)
	if !m.running || m.opName != engine.OpName(engine.RecycleWorktree{}) {
		t.Fatalf("running=%v opName=%q, want a running RecycleWorktree", m.running, m.opName)
	}
	// recycleInto is the one dispatch path; it reads m.recycleBranch, so the
	// op carried "loose" — pin that on the pure builder too.
	if op := recycleOpFor("/repo-wt/other", m.recycleBranch); op.Branch != "loose" || op.Dir != "/repo-wt/other" {
		t.Fatalf("recycleOpFor = %+v", op)
	}
}

func TestRecycleOptionLabelsTranslated(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"commit", "discard", "abort"} {
		if optionDisplayName(v) == "" {
			t.Errorf("optionDisplayName(%q) empty", v)
		}
	}
}

func TestRecycleOpRefreshesBranchesAndWorktrees(t *testing.T) {
	t.Parallel()
	got := opAffectedSources(engine.RecycleWorktree{})
	want := map[sourceKey]bool{srcBranches: true, srcWorktrees: true, srcFeed: true} // a commit recycle adds a commit: the feed must reload too
	for _, s := range got {
		delete(want, s)
	}
	if len(want) > 0 {
		t.Fatalf("opAffectedSources(RecycleWorktree) = %v, missing %v", got, want)
	}
}

// The branch column is aligned: every row's branch name starts at the same
// column whatever its path's elided width, so the picker reads as a table.
func TestRecyclePickerBranchColumnAligned(t *testing.T) {
	t.Parallel()
	m := recycleModel()
	m.width, m.height = 120, 40
	m.worktrees = []model.Worktree{
		{Path: "/repo", Branch: "main"},
		{Path: "/a", Branch: "short"},
		{Path: "/tmp/claude-1000/-mnt-t-others-gigagit/22501cfc-34d0-48fe-8b7d-ff6528344fbf/scratchpad/rc/wt", Branch: "wt-branch"},
		{Path: "/repo-wt/det", Detached: true},
	}
	row, _ := rowByID(availableActions(m), "recycle-worktree")
	nm, _ := row.run(m)
	m = nm.(Model)
	cols := map[int]bool{}
	for _, r := range m.actionMenu.rows {
		i := strings.LastIndex(r.label, "  ")
		if i < 0 {
			t.Fatalf("row %q has no column gap", r.label)
		}
		cols[len([]rune(r.label[:i+2]))] = true
	}
	if len(cols) != 1 {
		t.Fatalf("branch names start at %d different columns: %v", len(cols), cols)
	}
}

func TestRecycleRowLabelHasNoEllipsis(t *testing.T) {
	t.Parallel()
	m := recycleModel()
	row, _ := rowByID(availableActions(m), "recycle-worktree")
	if strings.Contains(row.label, "…") {
		t.Fatalf("label %q must not end in an ellipsis", row.label)
	}
}
