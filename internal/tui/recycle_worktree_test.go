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
	want := map[sourceKey]bool{srcBranches: true, srcWorktrees: true}
	for _, s := range got {
		delete(want, s)
	}
	if len(want) > 0 {
		t.Fatalf("opAffectedSources(RecycleWorktree) = %v, missing %v", got, want)
	}
}
